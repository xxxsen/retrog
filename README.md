# retrog

`retrog` 是一个面向 Pegasus 元数据的命令行工具集，用来：

- 解析本地 `metadata.pegasus.txt` 与 ROM/媒体文件；
- 将媒体资产上传到兼容 S3 的对象存储；
- 把整理后的 ROM 元信息写入本地 SQLite；
- 根据 ROM 哈希输出结构化 JSON 数据；
- 在调试阶段一键清空对象存储。
- 把 meta.db 中的元数据回填至 retrom PostgreSQL 数据库。

工程使用 Go 开发，模块名为 `github.com/xxxsen/retrog`，支持 `go install` 直接安装。

---

## 安装

```bash
go install github.com/xxxsen/retrog/cmd/retrog@latest
```

或克隆仓库后构建：

```bash
git clone https://github.com/xxxsen/retrog.git
cd retrog
go build ./cmd/retrog
```

生成的可执行文件为 `retrog`（Windows 上为 `retrog.exe`）。

---

## 配置

CLI 会按以下优先级查找 JSON 配置文件：

1. `--config <path>`
2. `./config.json`
3. `/etc/config.json`

配置格式：

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
  },
  "db": "./meta.db"
}
```

- **s3**：用于上传/下载媒体资源的对象存储；所有对象以 `media/<md5><ext>` 命名。
- **db**：SQLite 文件路径，存放解析后的 ROM 元数据。

加载配置后，CLI 会初始化：

1. S3 客户端（通过 `github.com/xxxsen/common/storage` 抽象层）；
2. SQLite 连接（`github.com/xxxsen/common/database/sqlite`），并确保存在表 `retro_game_meta_tab`。

---

## 输入目录结构

`upload` 命令要求 ROM 根目录形如：

```
roms/
  GB/
    metadata.pegasus.txt
    超级马里奥1.gb
    media/
      超级马里奥1/
        boxfront.png
        video.mp4
```

- 每个分类目录必须包含一份 Pegasus 元数据文件 `metadata.pegasus.txt`。
- `game` 节点的 `file:` 字段列出 ROM 文件名；工具会在同目录下查找这些文件并计算 MD5。
- 媒体文件优先读取 `assets.*` 描述（如 `assets.box_front: media/kof96ae20/boxfront.jpg`），否则回退到 `media/<rom文件名>/` 下按前缀匹配。

---

## 生成的数据

SQLite 表 `retro_game_meta_tab` 结构：

| 字段         | 类型        | 说明                              |
|--------------|-------------|-----------------------------------|
| id           | INTEGER PK  | 自增主键                          |
| rom_hash     | VARCHAR(32) | ROM 文件 MD5，唯一约束            |
| rom_name     | VARCHAR(128)| 清洗后的展示名称（`cleanGameName`）|
| rom_desc     | VARCHAR(1024)| 规整后的描述（`cleanDescription`）|
| rom_size     | INTEGER     | ROM 文件字节数                    |
| create_time  | BIGINT      | 首次写入时间（Unix 秒）           |
| update_time  | BIGINT      | 最近更新                           |
| ext_info     | VARCHAR(2048)| JSON：`{"media":[{type,hash,ext,size},...]}` |

媒体文件上传后命名为 `<md5><ext>` 并存放在对象存储的 `media/` 前缀下；元数据不会记录桶信息，只保留文件名、类型与大小。

---

## 命令总览

所有命令共享 `--config` 全局参数，并在执行前自动完成 S3/SQLite 初始化与统一的 `Init → PreRun → Run → PostRun` 生命周期。

### 1. `upload`

```
retrog upload \
  --dir <rom_root> \
  [--cat cat1,cat2]
```

行为：

1. 扫描 `--dir` 下的子目录；若指定 `--cat`，仅处理同名目录。
2. 解析每个目录的 `metadata.pegasus.txt`：
   - `cleanGameName` 将名称规范化（保留字母数字，空格转为 `-`）；
   - `cleanDescription` 统一全半角、压缩多余空白及连续标点；
   - `file:` 列表中的每个 ROM 文件都会单独生成记录，使用文件内容的 MD5 作为 `rom_hash`。
3. 为媒体类型 `boxart` / `boxfront` / `screenshot` / `video` / `logo` 搜索文件：
   - 先查看 `assets.*` 显式路径（支持相对/绝对路径）；
   - 否则在 `media/<ROM文件名（无扩展）>/` 下按前缀匹配；
   - 找到后计算 MD5，上传至 `media/<md5><ext>`，记录文件大小。
4. 以 50 条/批的方式写入 SQLite：先尝试 `INSERT`，若命中唯一键，再执行 `UPDATE`。

命令输出会记录处理目录及最终写入的条目数量。

### 2. `query`

```
retrog query \
  --hash <h1,h2,...> \
  [--meta ./override.db]
```

- `--hash` 为必填，支持逗号分隔多个哈希。
- `--meta` 可覆盖配置中的 SQLite 路径。
- 从 SQLite 读取匹配记录并输出 JSON，格式为 `{"hash":{"name":...}}`。
- 若某哈希不存在，会通过日志警告，但不会终止执行。

示例输出：

```json
{
  "37315071264cbc216e4ba379875ba1e1": {
    "name": "Super-Mario-Doctor",
    "desc": "……",
    "size": 82086,
    "media": [
      {"type": "logo", "hash": "7d049746b9f2076bff34227061a01603", "ext": ".png", "size": 34384},
      {"type": "video", "hash": "37315071264cbc216e4ba379875ba1e1", "ext": ".mp4", "size": 1309403}
    ]
  }
}
```

### 3. `clean-bucket`

```
retrog clean-bucket --force
```

- 调用存储客户端的 `ClearBucket` 方法删除桶内全部对象，主要用于测试环境重置。
- 必须显式传入 `--force`，否则命令会直接返回错误。

### 4. `patch-retrom-meta`

```
retrog patch-retrom-meta \
  --dblink "postgres://user:pass@host:5432/retrom?sslmode=disable" \
  [--root-mapping "/data/roms:/app/library"] \
  [--dryrun]
```

- 按 `game_files` / `games` 记录计算 ROM MD5，从本地 `meta.db` 查找元数据。
- 命中后将 `name`、`description`、封面/截图/视频等信息写入 retrom 的 `game_metadata`（`ON CONFLICT` upsert）。
- `--root-mapping` 用于将容器内路径映射为宿主机真实路径（格式 `host:container`），便于读取文件计算 MD5；未提供时使用原始路径。
- `--dryrun` 仅打印将执行的插入/更新动作，不对 PostgreSQL 写入。
- 可用于补齐缺失的元数据，维持 retrom 库与 meta.db 的一致性。

---

## 日志与错误处理

- 项目使用 `github.com/xxxsen/common/logutil` 封装的 zap 日志；默认输出到标准输出。
- 解析失败、缺少文件、S3/SQLite 操作失败等均会返回错误；非致命情况（如缺少某种媒体）会以 `Warn` 级别提示。
- 通过环境变量或配置的 `logger` 组件可调整日志等级。

---

## 开发提示

- Go 版本要求 ≥ 1.24。
- 常用依赖：`github.com/aws/aws-sdk-go-v2`、`github.com/didi/gendry/builder`、`github.com/xxxsen/common`。
- 新增命令时实现 `app.IRunner` 并在包 `init()` 中注册；CLI 会自动生成对应子命令。
- 运行 `go test ./...` 确认改动编译通过。

---

## License

MIT © 2024-present
