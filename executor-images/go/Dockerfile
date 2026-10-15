FROM golang:1.27.2-alpine

RUN addgroup -g 1001 sandbox && \
    adduser -D -u 1001 -G sandbox sandbox

WORKDIR /sandbox

ENV GOCACHE=/tmp/go-build

USER sandbox

CMD ["tail", "-f", "/dev/null"]
