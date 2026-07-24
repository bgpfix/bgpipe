FROM alpine:3.22
RUN apk add --no-cache bird
CMD ["bird", "-d", "-c", "/etc/bird.conf"]
