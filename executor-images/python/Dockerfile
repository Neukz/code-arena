FROM python:3.14.8-alpine

RUN addgroup -g 1001 sandbox && \
    adduser -D -u 1001 -G sandbox sandbox

WORKDIR /sandbox

USER sandbox

CMD ["tail", "-f", "/dev/null"]
