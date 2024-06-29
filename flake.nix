{
  description = "Flake for watgbridge";

  inputs = {
    nixpkgs.url = "github:nixos/nixpkgs?ref=nixos-unstable";
    flake-utils.url = "github:numtide/flake-utils";
    gomod2nix = {
        url = "github:nix-community/gomod2nix";
        inputs.nixpkgs.follows = "nixpkgs";
    };
  };

  outputs = { self, nixpkgs, flake-utils, gomod2nix }:
    flake-utils.lib.eachDefaultSystem (system:
      let
        pkgs = import nixpkgs {
         inherit system;
         overlays = [ gomod2nix.overlays.default ];
        };
      in
      with pkgs; {

        devShells.default = mkShell {
          name = "watgbridge-dev";
          nativeBuildInputs = [
            go
            gopls
            libwebp
            gomod2nix.packages."${system}".default
          ];

          shellHook = ''
              export GOPATH="$(git rev-parse --show-toplevel)/.go";
          '';
        };

        packages = rec {
          watgbridge = (pkgs.callPackage ./nix/pkgs/watgbridge-dev.nix {});
          default = watgbridge;
        };

      }
    );
}
