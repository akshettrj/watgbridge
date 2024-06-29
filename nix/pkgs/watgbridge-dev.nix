{ lib
, buildGoApplication
, libwebp
}:

buildGoApplication rec {
  pname = "watgbridge-dev";
  version = "1.8.2";

  pwd = ../../.;
  src = ../../.;

  buildInputs = [ libwebp ];

  ldflags = [ "-s" "-w" ];

  meta = with lib; rec {
    description = "A bridge between WhatsApp and Telegram written in Golang";
    homepage = "https://github.com/watgbridge/watgbridge";
    changelog = "${homepage}/compare/watgbridge-v${version}...main";
    license = licenses.mit;
    mainProgram = "watgbridge";
  };
}
