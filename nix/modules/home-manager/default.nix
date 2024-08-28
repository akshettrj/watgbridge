self: {
  lib,
  config,
  pkgs,
  ...
}: let
  inherit (pkgs.stdenv.hostPlatform) system;
  inherit (lib) mapAttrs' mkIf;
  cfg = config.services.watgbridge;

  package = self.packages."${system}".watgbridge;
in {
  options = {
    services.watgbridge = import ../commonOptions.nix { inherit lib package; };
  };

  config = mkIf cfg.enable {
    home.packages = [ cfg.package ];

    systemd.user.services = mapAttrs' (key: settings: let

      instanceName = (
        if settings.name != null then
          "watgbridge-${settings.name}"
        else
          "watgbridge-${key}"
      );
      watgbridgePackage = (
        if settings.package != null then
          settings.package
        else
          cfg.commonSettings.package
      );

      maxRuntime = (
        if settings.maxRuntime != null then
          settings.maxRuntime
        else
          cfg.commonSettings.maxRuntime
      );

      after = (
        if settings.after != null then
          settings.after
        else
          cfg.commonSettings.after
      );

    in {

      name = instanceName;

      value = mkIf settings.enable {
        Unit = {
          Description = "WaTgBridge service for '${instanceName}'";
          Documentation = "https://github.com/akshettrj/watbridge";
          After = [ "network.target" ] ++ lib.optionals (after != null) after;
        };

        Service = {
          ExecStart = ''${watgbridgePackage}/bin/watbridge'' lib.optionalString (settings.configPath != null) '' "${settings.configPath}"'';
          Restart = "on-failure";
        } // lib.optionalAttrs maxRuntime != null {
          RuntimeMaxSec = maxRuntime;
        };

        Install = { WantedBy = ["default.target"]; };
      };

    }) cfg.instances;
  };
}
