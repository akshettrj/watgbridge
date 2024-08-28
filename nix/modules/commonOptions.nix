{
  lib,
  package,
}: let
  inherit (lib) mkEnableOption mkOption types;
in {
  enable = mkEnableOption "WaTgBridge services";

  commonSettings.package = mkOption {
    type = types.package;
    default = package;
  };

  commonSettings.maxRuntime = mkOption {
    type = types.nullOr types.str;
    default = "1d";
  };

  commonSettings.requires = mkOption {
    type = types.nullOr (types.listOf types.str);
    default = null;
  };

  instances = mkOption {
    type = types.attrsOf(types.submodule {
      options = {

        enable = mkEnableOption "Enable the instance";

        name = mkOption {
          type = types.nullOr types.str;
          default = null;
          description = ''
            The name of the instance. The corresponding systemd service will be called `watgbridge-<name>.service`.

            By default, the key of the instance in the attrset will be used.
          '';
        };

        package = mkOption {
          type = types.nullOr types.package;
          default = null;
          description = ''
            The WaTgBridge package to use for the instance.

            By default the common package defined will be used.
          '';
        };

        configPath = mkOption {
          type = types.nullOr (types.either (types.str types.path));
          default = null;
          description = ''
            The path to the config file that will be loaded by WaTgBridge.

            By default, WaTgBridge loads config from config.yaml in the current working directory.
          '';
        };

        workingDirectory = mkOption {
          type = types.nullOr types.str;
          default = null;
          description = ''
            The directory from which the WaTgBridge binary will be executed.

            If not provided, it will use systemd's default working directory.
          '';
        };

        maxRuntime = mkOption {
          type = types.nullOr types.str;
          default = null;
          description = ''
            Max time in seconds (according to systemd's units) after which the service will be automatically restarted.

            If not provided, it will use the common setting (which is 1d, i.e. 1 day).
          '';
        };

        requires = mkOption {
          type = types.nullOf (types.listOf types.str);
          default = null;
          description = ''
            The systemd services to wait for before starting watbridge. "network.target" is added to the module itself. This option is meant to be used for stuff like Telegram Bot API service.

            If not provided, it will use the common setting
          '';
        };
      };
    });
  };
}
