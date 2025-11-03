# retrog

retrog 是一个 Go 编写的工具集，旨在帮助将 Pegasus ROM 资源迁移到兼容 S3 的对象存储，同时生成新的元数据、按需下载内容、清理测试环境，并辅助排查重复文件。

## 功能亮点

- **上传**：将 ROM 与媒体资源（截图、封面、视频等）上传至 S3，使用内容的 MD5 作为文件名，自动区分 `rom/` 与 `media/` 目录。
- **生成元数据**：解析 `metadata.pegasus.txt` 文件，输出统一的 JSON 元数据，方便其他启动器或工具引用。
- **按需同步**：依据生成的 JSON，按分类、类型（ROM / 媒体）把资源回写到本地，保持目录整洁。
- **清理环境**：一键清空测试专用的 S3 前缀，便于反复调试。
- **重复检测**：扫描目录，找出 MD5 / SHA1 一致或冲突的文件，避免资源冗余。
- **统一生命周期**：所有命令实现相同的 `Init → PreRun → Run → PostRun` 流程，并通过注册中心自动挂载到 CLI。

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
- `bucket`：统一存储桶，ROM 存放在 `rom/<md5>.<ext>`，媒体存放在 `media/<md5>.<ext>`。
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
        boxart.png
        video.mp4
```

- `metadata.pegasus.txt` 需为 Pegasus 格式，本工具至少读取 `game`、`file`、`description` 字段。
- ROM 文件应与元数据在同一目录，并且名称与 `file:` 指定的文件一致。
- 媒体资源可选，建议置于 `media/<rom文件名（不含扩展名）>/` 下，并以 `boxart`、`boxfront`、`screenshot`、`video`、`logo` 为前缀方便识别。

上传完成后会生成形如以下结构的 JSON：

```json
{
  "category": [
    {
      "cat_name": "Game Boy",
      "game_list": [
        {
          "name": "Super-Mario-Land",
          "desc": "...",
          "files": [
            {
              "hash": "b59d...",
              "ext": ".gb",
              "size": 262144,
              "file_name": "Super Mario Land.gb"
            }
          ],
          "media": {
            "boxart": "media/b59d...png",
            "video": "media/a381...mp4"
          }
        }
      ]
    }
  ]
}
```

注意：JSON 中记录的是相对路径（`rom/...`、`media/...`），真正的存储桶从配置中获取。

---

## CLI 命令

所有命令均在注册表中登记，CLI 启动时自动挂载，输出也会通过 `github.com/xxxsen/common/logutil` 进行结构化日志记录。

### `upload`

```
retrog upload --dir <rom根目录> --meta <输出meta.json> [--config <配置文件>]
```

- 扫描 `--dir` 下的所有子目录。
- 解析 `metadata.pegasus.txt` 并将 ROM / 媒体上传至 S3。
- 计算文件 MD5、大小等信息写入 meta。
- 元数据文件保存到 `--meta` 指定路径。

### `ensure`

```
retrog ensure \
  --meta meta.json \
  --cat "Game Boy" \
  --dir ./gb-pack \
  [--data rom|media|rom,media] \
  [--unzip] \
  [--config <配置文件>]
```

- 要求 `--dir` 不存在或为空，否则直接报错。
- 根据 `--data` 控制下载内容范围，默认 ROM 与媒体全部下载。
- ROM 文件名优先使用 meta 中记录的 `file_name`，多文件会自动生成 `_part_n` 后缀。
- `--unzip` 会解压只有单个文件的压缩包，并保留原始扩展名。

### `verify`

```
retrog verify --dir <待检查目录>
```

- 递归扫描目录，将带 `/media/` 的路径视作媒体，其余视作 ROM。
- 先按 MD5 聚类，再对重复项计算 SHA1 区分同 MD5 异内容的碰撞。
- 将重复组信息打印到控制台。

### `clean-bucket`

```
retrog clean-bucket --force [--config <配置文件>]
```

- 清空配置中指定桶内的所有对象（即 `rom/` 与 `media/` 前缀）。
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
