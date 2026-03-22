# Build stage
FROM golang:1.21-alpine AS builder

WORKDIR /app

# 安装依赖
RUN apk add --no-cache git

# 复制 go.mod 和 go.sum
COPY go.mod go.sum ./
RUN go mod download

# 复制源代码
COPY . .

# 构建所有二进制
RUN CGO_ENABLED=0 GOOS=linux go build -o /orchestrator cmd/orchestrator/main.go
RUN CGO_ENABLED=0 GOOS=linux go build -o /agent-runtime cmd/agent-runtime/main.go
RUN CGO_ENABLED=0 GOOS=linux go build -o /problem-cli cmd/problem-cli/main.go

# Runtime stage
FROM alpine:3.18

WORKDIR /app

# 安装必要工具
RUN apk add --no-cache ca-certificates tzdata

# 从构建阶段复制二进制
COPY --from=builder /orchestrator /app/orchestrator
COPY --from=builder /agent-runtime /app/agent-runtime
COPY --from=builder /problem-cli /app/problem-cli

# 复制配置和前端文件
COPY configs/ /app/configs/
COPY web/pixel/ /app/web/pixel/
COPY problems/ /app/problems/

# 暴露端口
EXPOSE 8080 8081

# 默认启动 orchestrator
ENTRYPOINT ["/app/orchestrator"]
