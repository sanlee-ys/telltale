<#
.SYNOPSIS
  Types keys into another process's console, from outside that console.

.DESCRIPTION
  This exists for ONE job: the unattended mode of record.ps1. A TUI quits on a
  keystroke, so an unattended capture of `telltale hud` needs a keystroke that
  no human is there to press. This script attaches to the recorder's hidden
  console and writes key events into its input buffer, which the recorder
  forwards down its pty exactly as if the key had been typed.

  RUN THIS AS ITS OWN PROCESS, never dot-sourced and never inline. Console
  attachment is per-process and a process may hold only one console, so the
  injection has to FreeConsole() first — and a FreeConsole() in the calling
  shell detaches THAT shell from the terminal the operator is sitting in.
  Measured 2026-08-16: doing this inline left the calling PowerShell alive but
  console-less, and the next native command died with "The handle is invalid"
  while getting the console mode. A child process contains the damage to a
  process that is about to exit anyway.

  Two Win32 details this depends on, both measured rather than assumed:

  - CONIN$, not GetStdHandle. After AttachConsole() the caller still holds the
    standard handles it inherited from its own parent, so GetStdHandle(STD_INPUT)
    returns those and WriteConsoleInput fails with ERROR_INVALID_HANDLE (6).
    Opening the CONIN$ pseudo-file names the attached console's own input
    buffer, whatever the standard handles point at.
  - A down/up pair per character. One down event is enough for a raw-mode
    reader, but leaving the buffer with an unmatched down event is a state a
    later reader can misread.

.PARAMETER ProcessId
  The recorder process whose console receives the keys.

.PARAMETER Keys
  The characters to type, in order.
#>
[CmdletBinding()]
param(
  [Parameter(Mandatory = $true)][int]$ProcessId,
  [Parameter(Mandatory = $true)][string]$Keys
)

$ErrorActionPreference = 'Stop'

Add-Type -Language CSharp -TypeDefinition @'
using System;
using System.Runtime.InteropServices;

public static class ConsoleKeyInjector {
  [DllImport("kernel32.dll", SetLastError = true)]
  static extern bool FreeConsole();
  [DllImport("kernel32.dll", SetLastError = true)]
  static extern bool AttachConsole(uint pid);
  [DllImport("kernel32.dll", SetLastError = true, CharSet = CharSet.Unicode)]
  static extern IntPtr CreateFileW(string name, uint access, uint share,
      IntPtr sec, uint disposition, uint flags, IntPtr template);
  [DllImport("kernel32.dll", SetLastError = true)]
  static extern bool CloseHandle(IntPtr h);
  [DllImport("kernel32.dll", SetLastError = true, CharSet = CharSet.Unicode)]
  static extern bool WriteConsoleInputW(IntPtr h, INPUT_RECORD[] buf, uint len, out uint written);

  [StructLayout(LayoutKind.Sequential)]
  struct KEY_EVENT_RECORD {
    public int bKeyDown;
    public ushort wRepeatCount;
    public ushort wVirtualKeyCode;
    public ushort wVirtualScanCode;
    public char UnicodeChar;
    public uint dwControlKeyState;
  }

  // EventType is a WORD, but the union that follows it is DWORD-aligned, so
  // the key record starts at offset 4 and not at offset 2.
  [StructLayout(LayoutKind.Explicit)]
  struct INPUT_RECORD {
    [FieldOffset(0)] public ushort EventType;
    [FieldOffset(4)] public KEY_EVENT_RECORD Key;
  }

  const ushort KEY_EVENT = 1;
  const uint GENERIC_RW = 0xC0000000;
  const uint SHARE_RW = 3;
  const uint OPEN_EXISTING = 3;

  public static string Send(uint pid, string s) {
    FreeConsole();
    if (!AttachConsole(pid)) {
      return "AttachConsole failed: " + Marshal.GetLastWin32Error();
    }
    IntPtr h = CreateFileW("CONIN$", GENERIC_RW, SHARE_RW, IntPtr.Zero, OPEN_EXISTING, 0, IntPtr.Zero);
    if (h == new IntPtr(-1)) {
      int e = Marshal.GetLastWin32Error();
      FreeConsole();
      return "open CONIN$ failed: " + e;
    }
    var records = new INPUT_RECORD[s.Length * 2];
    for (int i = 0; i < s.Length; i++) {
      for (int down = 1; down >= 0; down--) {
        int slot = i * 2 + (1 - down);
        records[slot].EventType = KEY_EVENT;
        records[slot].Key.bKeyDown = down;
        records[slot].Key.wRepeatCount = 1;
        records[slot].Key.UnicodeChar = s[i];
      }
    }
    uint written;
    bool ok = WriteConsoleInputW(h, records, (uint)records.Length, out written);
    int err = Marshal.GetLastWin32Error();
    CloseHandle(h);
    FreeConsole();
    return ok ? ("wrote " + written + " records") : ("WriteConsoleInput failed: " + err);
  }
}
'@

$result = [ConsoleKeyInjector]::Send([uint32]$ProcessId, $Keys)
Write-Output $result
if ($result -notlike 'wrote *') { exit 1 }
