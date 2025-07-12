{
  lib,
  buildGoModule,
  nix-filter,
}: let
  localSrc = nix-filter {
    name = "watgbridge";
    root = ../../.;
    exclude = [
      "flake.nix"
      "flake.lock"
      "README.md"
      "sample_config.yaml"
      "watgbridge.service.sample"
      ".github"
      "nix"
      "assets"
      ".envrc"
      ".gitignore"
      "Dockerfile"
      "LICENSE"
    ];
  };
in
  buildGoModule rec {
    pname = "watgbridge";
    version = lib.trim (builtins.readFile ../../state/version.txt);

    src = localSrc;

    vendorHash = "sha256-Zw55N6SHghRH53tBJmB4qTzsNcpNXe1k5U6mzuzeMbs=";

    subPackages = ["."];

    ldflags = [
      "-s"
      "-w"
    ];

    meta = with lib; rec {
      description = "A bridge between WhatsApp and Telegram written in Golang";
      homepage = "https://github.com/watgbridge/watgbridge";
      changelog = "${homepage}/compare/v${version}...main";
      license = licenses.mit;
      mainProgram = "watgbridge";
    };
  }
