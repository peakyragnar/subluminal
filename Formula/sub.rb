class Sub < Formula
  desc "Subluminal CLI - vendor-neutral data plane for AI agent tool execution"
  homepage "https://github.com/peakyragnar/subluminal"
  license "MIT"

  # Use HEAD for development builds until stable releases exist
  head "https://github.com/peakyragnar/subluminal.git", branch: "main"

  # Uncomment and update when cutting releases:
  # url "https://github.com/peakyragnar/subluminal/archive/refs/tags/v0.1.0.tar.gz"
  # sha256 "REPLACE_WITH_SHA256_OF_RELEASE_TARBALL"
  # version "0.1.0"

  depends_on "go" => :build

  def install
    # Determine version from git or use placeholder
    version_str = if build.head?
      Utils.safe_popen_read("git", "describe", "--tags", "--always", "--dirty").strip
    else
      version.to_s
    end

    ldflags = %W[
      -s -w
      -X main.version=#{version_str}
    ]
    system "go", "build", *std_go_args(ldflags: ldflags.join(" ")), "./cmd/sub"
  end

  test do
    # Verify binary runs and outputs version info
    output = shell_output("#{bin}/sub version")
    assert_match(/sub \S+/, output)
  end
end
