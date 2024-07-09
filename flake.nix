{
  description = "Flake for watgbridge";

  inputs = {
    nixpkgs.url = "github:nixos/nixpkgs?ref=nixos-unstable";
    flake-utils.url = "github:numtide/flake-utils";
    nix-filter.url = "github:numtide/nix-filter";
    gomod2nix = {
        url = "github:nix-community/gomod2nix";
        inputs.nixpkgs.follows = "nixpkgs";
    };
  };

  outputs = { self, nixpkgs, flake-utils, gomod2nix, nix-filter }:
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
        };

        packages = rec {
          watgbridge = (pkgs.callPackage ./nix/pkgs/watgbridge-dev.nix { inherit nix-filter; });
          default = watgbridge;
        };

      }
    );

  nixConfig = {
    extra-substituters = [ "https://watgbridge.cachix.org" ];
    extra-trusted-public-keys = [ "watgbridge.cachix.org-1:KSfgmbSBvXQTpUnoCj21vST7zgwpy3SbNfk0/nesR1Y=" ];
  };
}
