{ lib
, buildGoApplication
}:

buildGoApplication rec {
  pname = "watgbridge";
  version = "1.9.0";

  pwd = ../../.;
  src = ../../.;

  ldflags = [ "-s" "-w" ];

  meta = with lib; rec {
    description = "A bridge between WhatsApp and Telegram written in Golang";
    homepage = "https://github.com/watgbridge/watgbridge";
    changelog = "${homepage}/compare/watgbridge-v${version}...main";
    license = licenses.mit;
    mainProgram = "watgbridge";
  };
}
