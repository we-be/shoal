## Builds controller, minnow agent, or testsite.
## Usage: docker build --build-arg BINARY=controller .
FROM golang:1.24-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
ARG BINARY=controller
RUN CGO_ENABLED=0 go build -ldflags "-s -w" -o /shoal ./cmd/${BINARY}

FROM gcr.io/distroless/static-debian12
COPY --from=build /shoal /shoal
ENTRYPOINT ["/shoal"]
