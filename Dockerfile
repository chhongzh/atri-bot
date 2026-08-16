# syntax=docker/dockerfile:1

FROM alpine:3.24

ARG TZ=Asia/Shanghai
ENV TZ=${TZ}

RUN apk add --no-cache ca-certificates tzdata \
    && ln -snf /usr/share/zoneinfo/${TZ} /etc/localtime \
    && echo "${TZ}" > /etc/timezone

COPY atri-bot /usr/local/bin/atri-bot

WORKDIR /data

ENTRYPOINT ["atri-bot"]
