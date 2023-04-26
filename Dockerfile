FROM golang:1.19.8-alpine AS build

RUN apk --no-cache add gcc g++ make git libwebp-dev libwebp-tools ffmpeg imagemagick
WORKDIR /go/src/watgbridge
COPY . /go/src/watgbridge

RUN go build

CMD ["./watgbridge"]
