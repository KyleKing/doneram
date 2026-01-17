# Example from https://github.com/elves/elvish/blob/26a8bd5c4ee1eb5c0a2d53578d0368de2b8b3274/Dockerfile

# doner: golang:1.#.#-alpine*
FROM golang:1.22-alpine3.19 as builder

# doner: ignore
RUN apk add --no-cache --virtual build-deps make git

COPY . /go/src/src.elv.sh
RUN make -C /go/src/src.elv.sh get

# doner: alpine:3.#
FROM alpine:3.19

COPY --from=builder /go/bin/elvish /bin/elvish
CMD ["/bin/elvish"]
