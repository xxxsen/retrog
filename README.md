# retrog

retrog 是一个 Go 编写的工具集，旨在帮助将 Pegasus ROM 资源迁移到兼容 S3 的对象存储，同时生成新的元数据、按需下载内容、清理测试环境，并辅助排查重复文件。

## 功能亮点

- **上传**：扫描 ROM 根目录，只上传媒体资源（封面、截图、视频等）到 S3，文件名直接使用媒体文件的 MD5。
- **生成元数据**：读取各目录下的 `metadata.pegasus.txt`，输出以 ROM 哈希为键的 JSON，便于后续按需拉取媒体。
- **元数据查询**：通过 `query` 命令按 ROM 哈希筛选元数据并输出 JSON，便于快速查看资源信息。
- **清理环境**：提供清空测试桶的能力，方便重置环境。
- **统一生命周期**：所有命令遵循统一的 `Init → PreRun → Run → PostRun` 流程，并通过注册中心自动挂载到 CLI。

---

## 安装

```bash
git clone <repo-url>
cd retrog
go mod download
go build ./cmd/retrog
```

构建后的可执行文件为 `retrog`（Windows 上为 `retrog.exe`）。

---

## 配置

程序按照以下优先级读取配置（JSON）：

1. CLI 参数 `--config <path>`
2. 当前目录 `./config.json`
3. `/etc/config.json`

示例：

```json
{
  "s3": {
    "host": "https://minio.example.com",
    "bucket": "retro-archive",
    "region": "us-east-1",
    "access_key_id": "minioadmin",
    "secret_access_key": "minioadmin",
    "session_token": "",
    "force_path_style": true
  }
}
```

- `host`：S3/MinIO 访问地址，可使用 `https://` 或裸地址。
- `bucket`：统一存储桶，所有媒体资源会以 `<md5><ext>` 的形式上传到根目录。
- `force_path_style`：若对象存储需要 Path-Style 访问（例如 MinIO 默认配置），请设为 `true`。
- 若省略凭证，SDK 会使用环境变量或默认链路自动获取。

---

## 元数据与目录要求

```
roms/
  gb/
    metadata.pegasus.txt
    Super Mario Land.gb
    media/
      Super Mario Land/
        boxfront.png
        video.mp4
```

- `metadata.pegasus.txt` 需为 Pegasus 格式，至少包含 `game`、`file`、`description` 字段。
- ROM 文件需与元数据在同一目录，并与 `file:` 条目匹配；工具不会上传 ROM，只会读取其哈希值。
- 媒体资源建议放在 `media/<rom文件名（不含扩展名）>/` 子目录，并使用 `boxart`、`boxfront`、`screenshot`、`video`、`logo` 等前缀；若使用新的 `assets.*` 字段（例如 `assets.box_front: media/kof96ae20/boxfront.jpg`），则会直接按指定路径取文件。

`upload` 命令会生成一个以 ROM MD5 为键的 JSON：

```json
{
  "b59d...": {
    "name": "Super-Mario-Land",
    "desc": "......",
    "size": 262144,
    "media": [
      {
        "type": "boxart",
        "hash": "37315071264cbc216e4ba379875ba1e1",
        "ext": ".png",
        "size": 2048
      },
      {
        "type": "video",
        "hash": "a381...",
        "ext": ".mp4",
        "size": 8192
      }
    ]
  }
}
```

- JSON 值包含清洗后的 `name`、`desc`、ROM 大小以及媒体列表。
- 媒体文件记录了类型、哈希、扩展名和体积，可直接推导 S3 对象键 `<hash><ext>`。

---

## CLI 命令

所有命令均在注册表中登记，CLI 启动时自动挂载，输出也会通过 `github.com/xxxsen/common/logutil` 进行结构化日志记录。

### `upload`

```
retrog upload \
  --dir <rom根目录> \
  --meta <输出meta.json> \
  [--cat gb,fc] \
  [--config <配置文件>]
```

- 扫描 `--dir` 下的所有子目录；若指定 `--cat`（逗号分隔），则仅处理对应目录。
- 解析 `metadata.pegasus.txt`，只会上传媒体文件到 S3，ROM 仍保留在本地。
- 支持 `assets.*` 字段覆盖媒体路径；若未设置则回退到 `media/<rom名>/` 目录下匹配前缀文件。
- 媒体文件上传后使用 `<媒体文件MD5><扩展名>` 作为对象键，并记录在 meta 中。
- 生成以 ROM MD5 为键的 `meta.json`，包含清洗后的名称、描述、ROM 大小以及媒体条目（类型、哈希、扩展名、体积）。
- 输出文件写入 `--meta` 指定路径。

### `query`

```
retrog query \
  --meta meta.json \
  --hash <hash1,hash2>
```

- `--hash` 为必填，使用逗号分隔多个 ROM 哈希。
- 输出结果为 JSON，键为匹配的 ROM 哈希，值为对应的元数据。
- 若某个哈希在 meta 中不存在，会输出告警日志但不会中断命令。
- 可配合 `jq` 等工具对输出结果进一步处理。

### `clean-bucket`

```
retrog clean-bucket --force [--config <配置文件>]
```

- 清空配置中指定桶内的所有对象（主要用于移除已上传的媒体测试文件）。
- 为防误操作，必须显式携带 `--force`。

---

## 扩展命令

实现 `IRunner` 接口即可新增命令：

```go
type MyCommand struct { ... }

func (c *MyCommand) Name() string { return "something" }
func (c *MyCommand) Desc() string { return "简单描述" }
func (c *MyCommand) Init(f *pflag.FlagSet) { ... }
func (c *MyCommand) PreRun(ctx context.Context) error { ... }
func (c *MyCommand) Run(ctx context.Context) error { ... }
func (c *MyCommand) PostRun(ctx context.Context) error { ... }
```

在包初始化时向注册表登记：

```go
func init() {
    RegisterRunner("something", func() IRunner { return &MyCommand{} })
}
```

CLI 会自动根据注册表生成子命令。

---

## 开发与调试

- 提交前请运行 `gofmt -w $(find . -name '*.go')`。
- 建议使用 MinIO 搭建测试环境，`clean-bucket --force` 可快速清理环境。
- 遇到解析失败或缺失文件时，命令会通过 zap 日志提示。
- 可通过调整 `LOG_LEVEL` 查看更详细的调试信息。

---

## 依赖

- Go ≥ 1.24
- `github.com/aws/aws-sdk-go-v2` 及其子模块（S3）
- `github.com/spf13/cobra`
- `github.com/xxxsen/common/logutil` 与 `go.uber.org/zap`
- Pegasus 元数据文件 (`metadata.pegasus.txt`)

---

## License

MIT © [Your Name / Organization]
