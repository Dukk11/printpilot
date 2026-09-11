# build stage
FROM golang:1.22-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags "-s -w -X main.version=docker" -o /out/printpilot .

# run stage
FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/printpilot /printpilot
WORKDIR /data
VOLUME /data
EXPOSE 8787
ENTRYPOINT ["/printpilot"]
