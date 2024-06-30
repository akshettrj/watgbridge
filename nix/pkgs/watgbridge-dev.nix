{ lib
, buildGoApplication
}:

let

  localSrc = builtins.path {
    path = ../../.;
    name = "watgbridge";
    filter = path: type: (
      builtins.baseNameOf path != "flake.nix" &&
      builtins.baseNameOf path != "flake.lock" &&
      builtins.baseNameOf path != "README.md" &&
      builtins.baseNameOf path != "sample_config.yaml" &&
      builtins.baseNameOf path != "watgbridge.service.sample" &&
      builtins.baseNameOf path != ".github" &&
      builtins.baseNameOf path != "nix" &&
      builtins.baseNameOf path != "assets" &&
      builtins.baseNameOf path != ".envrc" &&
      builtins.baseNameOf path != ".gitignore" &&
      builtins.baseNameOf path != "Dockerfile" &&
      builtins.baseNameOf path != "LICENSE"
    );
  };

in buildGoApplication rec {
  pname = "watgbridge";
  version = "1.9.0";

  pwd = localSrc;
  src = localSrc;

  ldflags = [ "-s" "-w" ];

  meta = with lib; rec {
    description = "A bridge between WhatsApp and Telegram written in Golang";
    homepage = "https://github.com/watgbridge/watgbridge";
    changelog = "${homepage}/compare/watgbridge-v${version}...main";
    license = licenses.mit;
    mainProgram = "watgbridge";
  };
}
