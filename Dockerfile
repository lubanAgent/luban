# syntax=docker/dockerfile:1
FROM golang:1.25 AS build
WORKDIR /workspace/luban
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -o /out/luban .

FROM gcr.io/distroless/static-debian12:nonroot
WORKDIR /app
COPY --from=build /out/luban /app/luban
COPY --from=build /workspace/luban/conf/luban.yaml /app/conf/luban.yaml
COPY --from=build /workspace/luban/agents /app/agents
EXPOSE 8080
ENTRYPOINT ["/app/luban"]
