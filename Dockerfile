FROM golang:1.24-alpine AS build
WORKDIR /src
COPY go.mod ./
COPY . .
ARG BINARY=controller
RUN CGO_ENABLED=0 go build -o /shoal ./cmd/${BINARY}

FROM gcr.io/distroless/static-debian12
COPY --from=build /shoal /shoal
ENTRYPOINT ["/shoal"]
