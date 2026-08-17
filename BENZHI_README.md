# HyperLogLog 基数估算服务

这是一个纯 Go 的内存型 HyperLogLog 服务，用固定大小的寄存器数组近似估算数据流中的去重数量。它提供 HTTP JSON 接口，可创建具名草图、批量写入字符串、读取估算值和状态、合并同精度草图以及删除草图；响应会返回草图名称、精度、寄存器数量和估算结果。

## 标准构建、运行和测试

在项目根目录执行：

```bash
go build ./...               # 编译全部包
go run . --addr :8080        # 启动 HTTP 服务，监听本地 8080 端口
go test ./...                # 运行单元测试
go vet ./...                 # 执行静态检查
go run . --smoke-test        # 运行端到端自检并退出
```

项目入口位于根目录；服务默认监听 `:8080`，可用 `--addr` 指定其他地址。`--smoke-test` 不启动常驻服务，适合用于本地和容器中的快速自检。

## Benzhi 镜像

`build_benzhi_docker.sh` 接受两个参数：`IMAGE_NAME`（默认 `my-project`）和 `DOCKER_PLATFORM`（默认 `linux/amd64`）。脚本只使用 `benzhi.Dockerfile`。

```bash
bash ./build_benzhi_docker.sh task029-hll:amd64 linux/amd64
bash ./build_benzhi_docker.sh task029-hll:arm64 linux/arm64
docker run -it task029-hll:amd64
```

镜像固定使用 Go 1.26.3，并在构建阶段执行 `go build ./...`。
