FROM golang:1.23.0-alpine3.19 AS build

RUN apk --no-cache add gcc g++ make git libwebp-tools ffmpeg imagemagick
WORKDIR /go/src/watgbridge
COPY go.mod go.sum ./
RUN go mod download

COPY . ./
RUN go build

FROM alpine:3.19
RUN apk --no-cache add tzdata libwebp-tools ffmpeg imagemagick
WORKDIR /go/src/watgbridge
COPY --from=build /go/src/watgbridge/watgbridge .
CMD ["./watgbridge"]
