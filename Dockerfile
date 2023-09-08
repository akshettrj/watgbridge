FROM golang:1.19.8-alpine AS build

RUN apk --no-cache add gcc g++ make git libwebp-dev libwebp-tools ffmpeg imagemagick
WORKDIR /go/src/watgbridge
COPY go.mod go.sum ./
RUN go mod download

COPY . ./
RUN go build

FROM alpine
RUN apk --no-cache add tzdata libwebp-tools ffmpeg imagemagick
WORKDIR /go/src/watgbridge
COPY --from=build /go/src/watgbridge/watgbridge .
CMD ["./watgbridge"]
