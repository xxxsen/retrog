# retrog

`retrog` 是一个为 Pegasus 元数据整理而生的 CLI 工具集，帮助你：

- 扫描本地 ROM 目录，上传媒体资源到兼容 S3 的对象存储；
- 生成并维护 SQLite 版的 ROM 元数据；
- 按哈希查询元信息；
- 将 meta.db 中的内容补齐到 retrom 项目的 PostgreSQL 数据库；
- 在调试时一键清空对象存储。

---

## 安装

```bash
go install github.com/xxxsen/retrog/cmd/retrog@latest
```

或手动构建：

```bash
git clone https://github.com/xxxsen/retrog.git
cd retrog
go build ./cmd/retrog
```

全局配置默认按 `--config <path>` → `./config.json` → `/etc/config.json` 的优先级查找。

---

## 命令一览

所有子命令均支持 `--config` 指定配置，执行前会自动初始化 S3 与 SQLite。

### `scan`

```bash
retrog scan \
  --dir <rom_root>
```

- 递归遍历 `--dir` 下所有子目录，只要检测到 `metadata.pegasus.txt` 就会进行扫描；
- 解析元数据、上传封面/截图/视频等媒体文件至 S3（命名为 `media/<md5><ext>`）；
- 将清洗后的名称、描述、媒体信息写入 SQLite 的 `retro_game_meta_tab`；
- 同步 `developer`、`publisher`、`genre`、`release` 等字段到 `ext_info`，发行时间自动转换为 Unix 时间戳；
- 对仅包含单一 ROM 的 `.zip` 文件，同时记录压缩包 MD5 与内部文件 MD5 两条元数据。

### `query`

```bash
retrog query \
  --hash <hash1,hash2> \
  [--meta ./meta.db]
```

- 按 ROM MD5 查询 meta.db 中的记录并输出 JSON；
- 缺失的哈希会以日志方式提示，但不会终止命令；
- `--meta` 可覆盖配置内的 SQLite 路径。

### `clean`

```bash
retrog clean --force
```

- 调用 S3 客户端清空桶内对象，并删除 meta.db 中的所有记录；
- 为防误操作，必须显式携带 `--force`。

### `normalize`

```bash
retrog normalize --dir ./roms/gba [--unzip]
```

- 仅处理指定平台目录的顶层文件；
- 若检测到诸如 `xxx.gb` 直接位于平台目录下，会创建 `xxx/` 并移动到 `xxx/xxx.gb`；
- `--unzip` 可在 zip 仅包含单个 ROM 时自动解压并删除压缩包；
- 已位于目录中的游戏会被跳过。

### `patch-retrom-meta`

```bash
retrog patch-retrom-meta \
  --dblink "postgres://user:pass@host:5432/retrom?sslmode=disable" \
  [--root-mapping "/host/path:/app/library"] \
  [--dryrun]
```

- 读取 retrom 数据库的 `game_files`/`games` 表，根据文件 MD5 在 meta.db 中查找元信息；
- 命中后把名称、描述、媒体 URL 等写入 `game_metadata`（`ON CONFLICT` upsert）；
- `--root-mapping` 用于把容器内路径映射到宿主机真实路径，便于计算 MD5；
- `--dryrun` 只打印计划动作，不写数据库。

---

---

Powered by Codex
