package app

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/spf13/pflag"
	"github.com/xxxsen/common/logutil"
	"github.com/xxxsen/retrog/internal/sdk"
	"go.uber.org/zap"
)

// RomTestCommand validates ROM archives against DAT definitions using the SDK.
type RomTestCommand struct {
	datPath      string
	kind         string
	dirPath      string
	exts         string
	biosDir      string
	suppressWarn bool
}

func NewRomTestCommand() *RomTestCommand { return &RomTestCommand{} }

func (c *RomTestCommand) Name() string { return "rom-test" }

func (c *RomTestCommand) Desc() string { return "检查压缩包中的 ROM 是否符合 DAT 定义" }

func (c *RomTestCommand) Init(f *pflag.FlagSet) {
	f.StringVar(&c.datPath, "dat", "", "DAT 文件路径")
	f.StringVar(&c.kind, "kind", "fbneo", "DAT 类型，可选 fbneo, mame")
	f.StringVar(&c.dirPath, "dir", "", "待验证的压缩包目录，递归扫描")
	f.StringVar(&c.exts, "ext", "zip,7z", "扫描扩展名，逗号分隔，例如 zip,7z")
	f.StringVar(&c.biosDir, "bios", "", "BIOS 目录，用于补全 romof/clone 依赖")
	f.BoolVar(&c.suppressWarn, "suppress-warn", true, "是否隐藏警告信息")
}

func (c *RomTestCommand) PreRun(ctx context.Context) error {
	kind := strings.ToLower(strings.TrimSpace(c.kind))
	if kind == "" {
		return errors.New("rom-test requires --kind")
	}
	if kind != "fbneo" && kind != "mame" {
		return fmt.Errorf("unsupported kind: %s", kind)
	}
	if strings.TrimSpace(c.dirPath) == "" {
		return errors.New("rom-test requires --dir")
	}
	if strings.TrimSpace(c.datPath) == "" {
		return errors.New("rom-test requires --dat")
	}
	if _, err := parseExts(c.exts); err != nil {
		return err
	}
	logutil.GetLogger(ctx).Info("starting rom-test",
		zap.String("dat", c.datPath),
		zap.String("kind", c.kind),
		zap.String("dir", c.dirPath),
		zap.String("bios_dir", c.biosDir),
		zap.String("exts", c.exts),
		zap.Bool("suppress_warn", c.suppressWarn),
	)
	return nil
}

func (c *RomTestCommand) Run(ctx context.Context) error {
	logger := logutil.GetLogger(ctx)

	var tester sdk.IRomTestSDK
	var err error
	switch strings.ToLower(strings.TrimSpace(c.kind)) {
	case "fbneo":
		tester, err = sdk.NewFBNeoTestSDK(c.datPath)
	case "mame":
		tester, err = sdk.NewMameTestSDK(c.datPath)
	default:
		err = fmt.Errorf("unsupported kind: %s", c.kind)
	}
	if err != nil {
		return err
	}

	exts, err := parseExts(c.exts)
	if err != nil {
		return err
	}

	result, err := tester.TestDir(stdContextAdapter{ctx}, c.dirPath, c.biosDir, exts)
	if err != nil {
		return err
	}

	failCount := 0
	for _, item := range result.List {
		path := item.FilePath
		parentMissing := hasMissingParent(item.ParentList)
		hasRed := len(item.RedSubRomResultList) > 0
		hasYellow := len(item.YellowSubRomResultList) > 0
		if c.suppressWarn {
			hasYellow = false
		}
		status := "test ok"
		if hasRed {
			status = "test error"
		} else if hasYellow || parentMissing {
			status = "test warn"
		}
		label := formatParentLabel(item.ParentList)
		fmt.Printf("%s -- %s%s\n", path, status, label)

		if hasRed || hasYellow {
			if !c.suppressWarn {
				for _, r := range item.YellowSubRomResultList {
					printSubResult("warn", r)
				}
			}
			for _, r := range item.RedSubRomResultList {
				printSubResult("error", r)
			}
		}
		if hasRed {
			failCount++
		}
	}

	if failCount > 0 {
		return fmt.Errorf("rom check failed for %d file(s)", failCount)
	}

	logger.Info("rom check passed", zap.Int("file_count", len(result.List)))
	return nil
}

func (c *RomTestCommand) PostRun(ctx context.Context) error { return nil }

func init() {
	RegisterRunner("rom-test", func() IRunner { return NewRomTestCommand() })
}

func parseExts(exts string) ([]string, error) {
	if strings.TrimSpace(exts) == "" {
		return nil, errors.New("ext cannot be empty")
	}
	parts := strings.Split(exts, ",")
	var out []string
	for _, part := range parts {
		trimmed := strings.TrimSpace(part)
		trimmed = strings.TrimPrefix(trimmed, ".")
		if trimmed == "" {
			continue
		}
		out = append(out, trimmed)
	}
	if len(out) == 0 {
		return nil, errors.New("invalid ext value")
	}
	return out, nil
}

type stdContextAdapter struct{ context.Context }

func (a stdContextAdapter) Done() <-chan struct{} { return a.Context.Done() }
func (a stdContextAdapter) Err() error            { return a.Context.Err() }

func hasMissingParent(parents []sdk.ParentInfo) bool {
	for _, p := range parents {
		if !p.Exist {
			return true
		}
	}
	return false
}

func formatParentLabel(parents []sdk.ParentInfo) string {
	if len(parents) == 0 {
		return ""
	}
	var parentParts []string
	var biosParts []string
	for _, p := range parents {
		if p.Name == "" {
			continue
		}
		name := p.Name
		if !p.Exist {
			name = fmt.Sprintf("%s missing", p.Name)
		}
		if p.IsBios {
			biosParts = append(biosParts, name)
		} else {
			parentParts = append(parentParts, name)
		}
	}
	hasParent := len(parentParts) > 0
	hasBios := len(biosParts) > 0
	if !hasParent && !hasBios {
		return ""
	}
	switch {
	case hasParent && hasBios:
		return fmt.Sprintf("(parent: %s, bios: %s)", strings.Join(parentParts, ", "), strings.Join(biosParts, ", "))
	case hasBios:
		return fmt.Sprintf("(bios: %s)", strings.Join(biosParts, ", "))
	default:
		return fmt.Sprintf("(parent: %s)", strings.Join(parentParts, ", "))
	}
}

func printSubResult(level string, r *sdk.SubRomFileTestResult) {
	if r == nil || r.SubRom == nil {
		return
	}
	label := level
	if level == "error" {
		label = "\033[31merror\033[0m"
	}
	name := r.SubRom.NormalizedName()
	crc := r.SubRom.CRC
	size := r.SubRom.Size
	reason := r.TestMessage
	if reason == "" {
		switch level {
		case "warn":
			reason = "warning"
		case "error":
			reason = "error"
		}
	}
	fmt.Printf("- %s: %s %s %d => %s\n", label, name, crc, size, reason)
}
