FROM golang:1.24

WORKDIR /build
COPY . ./
RUN CGO_ENABLED=0 go build -a -tags netgo -ldflags '-w' -o retrog ./cmd/retrog

FROM alpine:3.17
COPY --from=0 /build/retrog /bin/

ENTRYPOINT [ "/bin/retrog" ]