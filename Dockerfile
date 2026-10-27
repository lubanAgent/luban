# syntax=docker/dockerfile:1
FROM golang:1.25 AS build
ENV GOPRIVATE=git.yukework.com
WORKDIR /workspace/luban
COPY go.mod go.sum ./
RUN --mount=type=secret,id=netrc,target=/root/.netrc go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -o /out/luban .
RUN mkdir -p /runtime/log

FROM gcr.io/distroless/static-debian12:nonroot
WORKDIR /app
COPY --from=build /out/luban /app/luban
COPY --from=build /workspace/luban/conf/luban.yaml /app/conf/luban.yaml
COPY --from=build /workspace/luban/agents /app/agents
COPY --from=build --chown=nonroot:nonroot /runtime/log /app/log
EXPOSE 8080
ENTRYPOINT ["/app/luban"]
