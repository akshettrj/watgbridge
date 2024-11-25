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

  outputs =
    {
      self,
      nixpkgs,
      flake-utils,
      gomod2nix,
      nix-filter,
    }:
    flake-utils.lib.eachDefaultSystem (
      system:
      let
        pkgs = import nixpkgs {
          inherit system;
          overlays = [ gomod2nix.overlays.default ];
        };
      in
      with pkgs;
      rec {

        devShells.default = mkShell {
          name = "watgbridge-dev";
          nativeBuildInputs = [
            go
            gopls
            delve
            libwebp
            gomod2nix.packages."${system}".default
            sqlite
          ];
          hardeningDisable = [ "fortify" ];
        };

        apps.default = {
          type = "app";
          program = "${packages.default}/bin/watgbridge";
        };

        packages = rec {
          watgbridge = (pkgs.callPackage ./nix/pkgs/watgbridge-dev.nix { inherit nix-filter; });
          default = watgbridge;
        };

        overlay = final: prev: {
          watgbridge = (pkgs.callPackage ./nix/pkgs/watgbridge-dev.nix { inherit nix-filter; });
        };

      }
    );

  nixConfig = {
    extra-substituters = [ "https://watgbridge.cachix.org" ];
    extra-trusted-public-keys = [
      "watgbridge.cachix.org-1:KSfgmbSBvXQTpUnoCj21vST7zgwpy3SbNfk0/nesR1Y="
    ];
  };
}
