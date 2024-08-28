self: {
  lib,
  config,
  pkgs,
  ...
}: let
  inherit (pkgs.stdenv.hostPlatform) system;
  inherit (lib) mkIf;
  cfg = config.services.watgbridge;

  package = self.packages."${system}".watgbridge;
in {
  options = {
    services.watgbridge = import ../commonOptions.nix { inherit lib package; };
  };

  config = mkIf cfg.enable {
    assertions = [{
      assertion = false;
      message = "The NixOS module is not complete yet. Use home-manager module for now if possible.";
    }];

    environment.systemPackages = [ cfg.package ];
  };
}
