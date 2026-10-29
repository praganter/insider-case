FROM golang:1.27-alpine AS build
WORKDIR /src
COPY go.mod ./
RUN go mod download
COPY . .
# `make test` builds this stage; a failing test fails the build.
FROM build AS test
RUN go test ./...

FROM build AS bin
# CMD selects which binary under cmd/ to build (api, worker, ...).
ARG CMD=api
RUN CGO_ENABLED=0 go build -o /out/app ./cmd/${CMD}

FROM alpine:3.22
COPY --from=bin /out/app /app
ENTRYPOINT ["/app"]
