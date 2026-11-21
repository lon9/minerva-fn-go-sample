FROM golang:1.22-alpine AS build
WORKDIR /src
COPY go.mod main.go ./
RUN CGO_ENABLED=0 go build -o /out/webapp .

FROM scratch
COPY --from=build /out/webapp /webapp
USER 65532:65532
EXPOSE 8080
ENTRYPOINT ["/webapp"]
