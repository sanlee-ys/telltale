# typed: false
# frozen_string_literal: true

# This file is maintained by GoReleaser. DO NOT EDIT.
#
# The first copy (2026-09-02) was written by hand in the shape goreleaser
# v2.17.1 writes, verified against a `goreleaser release --snapshot` run, with
# the sha256 values taken from the v0.2.0 release's checksums.txt. goreleaser
# overwrites this whole file at the next v* tag (packaging/README.md, "The
# Homebrew tap"), and that copy carries goreleaser's own header.
class Telltale < Formula
  desc "Dispatch room for several coding-agent CLIs, with an honest gauge underneath"
  homepage "https://github.com/sanlee-ys/telltale"
  version "0.2.0"
  license "MIT"

  on_macos do
    if Hardware::CPU.intel?
      url "https://github.com/sanlee-ys/telltale/releases/download/v0.2.0/telltale_0.2.0_darwin_amd64.tar.gz"
      sha256 "979e89d06469baf9c31d6505a08f8dda833ee3958e016dbae49692b169074f9c"

      define_method(:install) do
        bin.install "telltale"
      end
    end
    if Hardware::CPU.arm?
      url "https://github.com/sanlee-ys/telltale/releases/download/v0.2.0/telltale_0.2.0_darwin_arm64.tar.gz"
      sha256 "72d822388fc376a9796af0694dc797b00ededdb4a324951e372179a607e59a52"

      define_method(:install) do
        bin.install "telltale"
      end
    end
  end

  on_linux do
    if Hardware::CPU.intel? && Hardware::CPU.is_64_bit?
      url "https://github.com/sanlee-ys/telltale/releases/download/v0.2.0/telltale_0.2.0_linux_amd64.tar.gz"
      sha256 "86400fd3173f76325b94d8bae6ec6d850acabdcef3e7beb5f8ca21a07079b654"
      define_method(:install) do
        bin.install "telltale"
      end
    end
  end

  test do
    assert_match version.to_s, shell_output("#{bin}/telltale version")
  end
end
