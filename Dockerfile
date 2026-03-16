FROM golang:1.25-alpine3.22 AS build

RUN apk --no-cache add gcc g++ make git libwebp-tools ffmpeg imagemagick
WORKDIR /go/src/watgbridge
COPY go.mod go.sum ./
RUN go mod download

COPY . ./
RUN go build

FROM alpine:3.22
RUN apk --no-cache add tzdata libwebp-tools ffmpeg imagemagick
WORKDIR /go/src/watgbridge
COPY --from=build /go/src/watgbridge/watgbridge .
COPY docker/entrypoint.sh /docker/
RUN chmod +x /docker/entrypoint.sh
ENTRYPOINT ["/docker/entrypoint.sh"]
