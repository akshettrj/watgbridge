{ lib
, buildGoApplication
, enableFfmpeg ? true
, enableLibWebPTools ? true
, ffmpeg
, libwebp
, nix-filter
}:

let

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

in buildGoApplication rec {
  pname = "watgbridge";
  version = "1.10.1";

  pwd = localSrc;
  src = localSrc;

  buildInputs = [
  ] ++ lib.optionals enableFfmpeg [
    ffmpeg
  ] ++ lib.optionals enableLibWebPTools [
    libwebp
  ];

  ldflags = [ "-s" "-w" ];

  meta = with lib; rec {
    description = "A bridge between WhatsApp and Telegram written in Golang";
    homepage = "https://github.com/watgbridge/watgbridge";
    changelog = "${homepage}/compare/v${version}...main";
    license = licenses.mit;
    mainProgram = "watgbridge";
  };
}
