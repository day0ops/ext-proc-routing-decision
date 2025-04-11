FROM alpine:3.21

# TARGETOS and TARGETARCH are set automatically when --platform is provided.
ARG TARGETOS
ARG TARGETARCH
ARG NAME

ADD "https://curl.haxx.se/ca/cacert.pem" "/etc/ssl/certs/ca-certificates.crt"
ADD "./bin/${TARGETOS}_${TARGETARCH}/${NAME}" "/server"
RUN apk add --no-cache curl

# Create a non-root user to run this.
RUN addgroup rps
RUN adduser -S -G rps 1019

USER 1019
ENTRYPOINT ["/server"]