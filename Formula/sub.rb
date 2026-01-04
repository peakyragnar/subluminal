class Sub < Formula
  desc "Subluminal CLI"
  homepage "https://github.com/subluminal/subluminal"
  url "https://github.com/subluminal/subluminal/archive/refs/heads/main.tar.gz"
  version "0.0.0"
  sha256 :no_check

  depends_on "go" => :build

  def install
    ldflags = "-s -w -X main.version=#{version}"
    system "go", "build", *std_go_args(ldflags: ldflags), "./cmd/sub"
  end

  test do
    assert_match "sub #{version}", shell_output("#{bin}/sub version")
  end
end
