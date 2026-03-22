# LLVM 编译器插件接入指南

## 概述

本插件基于 LLVM/Clang 工具链，支持 C 和 C++ 代码的编译。

## 前置条件

确保系统已安装 Clang：

```bash
# Ubuntu/Debian
apt install clang

# CentOS/RHEL
yum install clang

# macOS
xcode-select --install

# 验证安装
clang --version
```

## 快速接入

### 1. 实现接口

所有编译器需实现 `compiler.CompilerPlugin` 接口：

```go
type CompilerPlugin interface {
    Name() string
    Version() string
    SupportedLanguages() []string
    OptimizationLevels() []string
    Compile(ctx context.Context, req *CompileRequest) (*CompileResult, error)
    Validate() error
}
```

### 2. 注册编译器

```go
import (
    "oj-platform/multi-agent/pkg/compiler"
    "oj-platform/multi-agent/pkg/compiler/llvm"
)

func init() {
    compiler.Register(llvm.New())
}
```

### 3. 配置文件

在 `configs/compilers.yaml` 中添加：

```yaml
compilers:
  - name: llvm-clang
    enabled: true
    languages: [c, cpp]
    optimization_levels: [-O0, -O1, -O2, -O3, -Os, -Oz]
    timeout: 30s
```

## 使用示例

```go
// 通过工厂按语言编译
result, err := compiler.GetByLanguage("c")

// 或指定编译器
plugin, _ := compiler.Get("llvm-clang")
result, err := plugin.Compile(ctx, &compiler.CompileRequest{
    Language:          "c",
    SourceCode:        `#include <stdio.h>\nint main() { printf("hello"); }`,
    OptimizationLevel: "-O2",
    Timeout:           10 * time.Second,
})
```

## 新编译器接入流程

如需接入新编译器（如 Rust、Java），请参照以下步骤：

```
pkg/compiler/
├── interface.go        # 接口定义（不要修改）
├── base.go             # 沙箱基类（可复用）
├── your_compiler/      # 你的编译器目录
│   ├── compiler.go     # 接口实现
│   └── README.md       # 接入说明
```

1. 创建目录 `pkg/compiler/<name>/`
2. 实现 `CompilerPlugin` 接口
3. 添加配置到 `configs/compilers.yaml`
4. 在主程序中 `compiler.Register(your.New())`
5. 提交 PR，附带测试用例

## 测试要求

- 单元测试覆盖率 > 80%
- 通过沙箱安全验证（无网络访问、资源限制）
- 提供编译成功和失败的测试用例
