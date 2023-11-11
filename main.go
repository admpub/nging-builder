package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/admpub/confl"
	"github.com/webx-top/com"
)

// usage:
// 1. go run main.go min
// 2. go run main.go linux_arm64 min

var p = buildParam{}

const version = `v0.0.5`

var c = Config{
	GoVersion:    `1.21.4`,
	Executor:     `nging`,
	NgingVersion: `5.2.3`,
	NgingLabel:   `stable`,
	Project:      `github.com/admpub/nging`,
	VendorMiscDirs: map[string][]string{
		`*`: {
			`vendor/github.com/nging-plugins/caddymanager/template/`,
			`vendor/github.com/nging-plugins/collector/template/`,
			`vendor/github.com/nging-plugins/collector/public/assets/`,
			`vendor/github.com/nging-plugins/dbmanager/template/`,
			`vendor/github.com/nging-plugins/dbmanager/public/assets/`,
			`vendor/github.com/nging-plugins/ddnsmanager/template/`,
			`vendor/github.com/nging-plugins/dlmanager/template/`,
			`vendor/github.com/nging-plugins/frpmanager/template/`,
			`vendor/github.com/nging-plugins/ftpmanager/template/`,
			`vendor/github.com/nging-plugins/servermanager/template/`,
			`vendor/github.com/nging-plugins/sshmanager/template/`,
			`vendor/github.com/nging-plugins/webauthn/template/`,
		},
		`linux`: {
			`vendor/github.com/nging-plugins/firewallmanager/template/`,
		},
		`!linux`: {},
	},
	BuildTags: []string{`bindata`, `sqlite`},
	CopyFiles: []string{`config/ua.txt`, `config/config.yaml.sample`, `data/ip2region`, `config/preupgrade.*`},
	MakeDirs:  []string{`public/upload`, `config/vhosts`, `data/logs`},
	Compiler:  `xgo`,
}

var targetNames = map[string]string{
	`linux_386`:     `linux/386`,
	`linux_amd64`:   `linux/amd64`,
	`linux_arm5`:    `linux/arm-5`,
	`linux_arm6`:    `linux/arm-6`,
	`linux_arm7`:    `linux/arm-7`,
	`linux_arm64`:   `linux/arm64`,
	`darwin_amd64`:  `darwin/amd64`,
	`darwin_arm64`:  `darwin/arm64`,
	`windows_386`:   `windows/386`,
	`windows_amd64`: `windows/amd64`,
}

var armRegexp = regexp.MustCompile(`/arm`)
var configFile = `./builder.conf`
var showVersion bool

func main() {
	flag.StringVar(&configFile, `conf`, configFile, `--conf `+configFile)
	flag.BoolVar(&showVersion, `version`, false, `--version`)
	defaultUsage := flag.Usage
	flag.Usage = func() {
		defaultUsage()
		fmt.Println()
		fmt.Println(`Command Format:`, os.Args[0], `[os_arch]`, `[min]`)
	}
	flag.Parse()

	if showVersion {
		fmt.Println(version)
		return
	}
	isGenConfig := len(flag.Args()) == 1 && com.InSlice(`genConfig`, flag.Args())

	configInFile := Config{}
	_, err := confl.DecodeFile(configFile, &configInFile)
	if err != nil && !isGenConfig {
		com.ExitOnFailure(err.Error(), 1)
	}
	configInFile.apply()
	p.ProjectPath, err = com.GetSrcPath(p.Project)
	if err != nil && !isGenConfig {
		com.ExitOnFailure(err.Error(), 1)
	}
	p.WorkDir = strings.TrimSuffix(strings.TrimSuffix(p.ProjectPath, `/`), p.Project)
	var targets []string
	var armTargets []string
	addTarget := func(target string, notNames ...bool) {
		if len(notNames) == 0 || !notNames[0] {
			target = getTarget(target)
			if len(target) == 0 {
				return
			}
		}
		if armRegexp.MatchString(target) {
			armTargets = append(armTargets, target)
		} else {
			targets = append(targets, target)
		}
	}
	args := make([]string, len(flag.Args()))
	copy(args, flag.Args())
	var minify bool
	var target string
	switch len(args) {
	case 2:
		minify = isMinified(args[1])
		target = args[0]
		addTarget(target)
	case 1:
		switch {
		case isMinified(args[0]):
			minify = true
			for _, t := range targetNames {
				addTarget(t, true)
			}
		case args[0] == `genConfig`:
			b, err := confl.Marshal(c)
			if err != nil {
				com.ExitOnFailure(err.Error(), 1)
			}
			err = os.WriteFile(configFile, b, os.ModePerm)
			if err != nil {
				com.ExitOnFailure(err.Error(), 1)
			}
			com.ExitOnSuccess(`successully generate config file: ` + configFile)
			return
		case args[0] == `makeGen`:
			makeGenerateCommandComment(c)
			return
		case args[0] == `version`:
			fmt.Println(version)
			return
		default:
			addTarget(args[0])
		}
	case 0:
		for _, t := range targetNames {
			addTarget(t, true)
		}
	default:
		com.ExitOnFailure(`invalid parameter`)
	}
	makeGenerateCommandComment(c)
	fmt.Println(`ConfFile	:	`, configFile)
	fmt.Println(`WorkDir		:	`, p.WorkDir)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	err = os.Chdir(p.ProjectPath)
	if err != nil {
		com.ExitOnFailure(err.Error(), 1)
	}
	p.NgingCommitID = execGitCommitIDCommand(ctx)
	p.NgingBuildTime = time.Now().Format(`20060102150405`)
	if minify {
		p.MinifyFlags = []string{`-s`, `-w`}
	}
	distPath := filepath.Join(p.ProjectPath, `dist`)
	err = com.MkdirAll(distPath, os.ModePerm)
	if err != nil {
		com.ExitOnFailure(err.Error(), 1)
	}
	fmt.Println(`DistPath	:	`, distPath)
	allTargets := append(targets, armTargets...)
	if len(target) > 0 && len(allTargets) == 0 {
		com.ExitOnFailure(`Error		:	 Unsupported target ` + fmt.Sprintf(`%q`, target) + "\n")
	}
	fmt.Printf("Building %s for %+v\n", p.Executor, allTargets)
	singleFileMode := isSingleFile()
	for _, target := range allTargets {
		parts := strings.SplitN(target, `/`, 2)
		if len(parts) != 2 {
			continue
		}
		pCopy := p
		pCopy.Target = target
		pCopy.PureGoTags = []string{`osusergo`}
		osName := parts[0]
		archName := parts[1]
		if singleFileMode {
			pCopy.ReleaseDir = distPath
		} else {
			pCopy.ReleaseDir = filepath.Join(distPath, p.Executor+`_`+osName+`_`+archName)
			err = com.MkdirAll(pCopy.ReleaseDir, os.ModePerm)
			if err != nil {
				com.ExitOnFailure(err.Error(), 1)
			}
		}
		pCopy.goos = osName
		pCopy.goarch = archName
		if osName != `darwin` {
			pCopy.LdFlags = []string{`-extldflags`, `'-static'`}
		}
		if osName != `windows` {
			pCopy.PureGoTags = append(pCopy.PureGoTags, `netgo`)
		} else {
			pCopy.Extension = `.exe`
		}
		execGenerateCommand(ctx, pCopy)
		execBuildCommand(ctx, pCopy)
		normalizeExecuteFileName(pCopy, singleFileMode)
		if !singleFileMode {
			packFiles(pCopy)
		}
	}
}

func getTarget(target string) string {
	if t, y := targetNames[target]; y {
		return t
	}
	for _, t := range targetNames {
		if t == target {
			return t
		}
	}
	return ``
}

func isMinified(arg string) bool {
	return arg == `m` || arg == `min`
}

func isSingleFile() bool {
	isSingle := len(p.CopyFiles) == 0 && len(p.MakeDirs) == 0
	if !isSingle {
		return isSingle
	}
	isSingle = len(p.VendorMiscDirs) == 0
	if isSingle {
		return isSingle
	}
	for _, items := range p.VendorMiscDirs {
		if len(items) > 0 {
			return false
		}
	}
	return isSingle
}

type buildParam struct {
	Config
	Target         string //${GOOS}/${GOARCH}
	ReleaseDir     string
	Extension      string
	PureGoTags     []string
	NgingBuildTime string
	NgingCommitID  string
	MinifyFlags    []string
	LdFlags        []string
	ProjectPath    string
	WorkDir        string
	goos           string
	goarch         string
}

func (p buildParam) genLdFlagsString() string {
	ldflags := []string{}
	ldflags = append(ldflags, p.MinifyFlags...)
	ldflags = append(ldflags, p.LdFlags...)
	return `-X main.BUILD_TIME=` + p.NgingBuildTime + ` -X main.COMMIT=` + p.NgingCommitID + ` -X main.VERSION=` + p.NgingVersion + ` -X main.LABEL=` + p.NgingLabel + ` -X main.BUILD_OS=` + p.goos + ` -X main.BUILD_ARCH=` + p.goarch + ` ` + strings.Join(ldflags, ` `)
}

func (p buildParam) genEnvVars() []string {
	env := []string{`GOOS=` + p.goos}
	parts := strings.SplitN(p.goarch, `-`, 2)
	if parts[0] == `arm` {
		env = append(env, `GOARCH=`+parts[0])
		if len(parts) == 2 {
			env = append(env, `GOARM=`+parts[1])
		}
	} else {
		env = append(env, `GOARCH=`+p.goarch)
	}
	return env
}

func execBuildCommand(ctx context.Context, p buildParam) {
	tags := []string{}
	tags = append(tags, p.PureGoTags...)
	tags = append(tags, p.BuildTags...)
	var args []string
	var env []string
	var workDir string
	var compiler string
	switch p.Compiler {
	case `go`:
		workDir = filepath.Join(p.WorkDir, p.Project)
		compiler = p.Compiler
		com.MkdirAll(p.ReleaseDir, os.ModePerm)
		args = []string{`build`,
			`-tags`, strings.Join(tags, ` `),
			`-ldflags`, p.genLdFlagsString(),
			`-o`, filepath.Join(p.ReleaseDir, p.Executor+`-`+p.goos+`-`+p.goarch),
		}
		env = append(env, os.Environ()...)
		env = append(env, p.genEnvVars()...)
		if p.CgoEnabled {
			env = append(env, `CGO_ENABLED=1`)
		} else {
			env = append(env, `CGO_ENABLED=0`)
		}
	case `xgo`:
		fallthrough
	default:
		workDir = p.WorkDir
		compiler = `xgo`
		image := p.GoImage
		if len(image) == 0 {
			image = `admpub/xgo:` + p.GoVersion
		} else {
			checkStr := image
			pos := strings.LastIndex(image, `/`)
			if pos > -1 {
				checkStr = image[pos:]
			}
			if !strings.Contains(checkStr, `:`) {
				image += `:` + p.GoVersion
			}
		}
		if len(p.GoProxy) == 0 {
			p.GoProxy = `https://goproxy.cn,direct`
		}
		args = []string{
			`-go`, p.GoVersion,
			`-goproxy`, p.GoProxy,
			`-image`, image,
			`-targets`, p.Target,
			`-dest`, p.ReleaseDir,
			`-out`, p.Executor,
			`-tags`, strings.Join(tags, ` `),
			`-ldflags`, p.genLdFlagsString(),
			`./` + p.Project,
		}
	}
	cmd := exec.CommandContext(ctx, compiler, args...)
	cmd.Dir = workDir
	cmd.Stdin = os.Stdin
	cmd.Stderr = os.Stderr
	cmd.Stdout = os.Stdout
	cmd.Env = env
	err := cmd.Run()
	if err != nil {
		com.ExitOnFailure(err.Error(), 1)
	}
}

func execGenerateCommand(ctx context.Context, p buildParam) {
	cmd := exec.CommandContext(ctx, `go`, `generate`)
	cmd.Dir = p.ProjectPath
	cmd.Stdin = os.Stdin
	cmd.Stderr = os.Stderr
	cmd.Stdout = os.Stdout
	cmd.Env = append(cmd.Env, os.Environ()...)
	cmd.Env = append(cmd.Env, p.genEnvVars()...)
	err := cmd.Run()
	if err != nil {
		com.ExitOnFailure(err.Error(), 1)
	}
}

func execGitCommitIDCommand(ctx context.Context) string {
	cmd := exec.CommandContext(ctx, `git`, `rev-parse`, `HEAD`)
	cmd.Dir = p.ProjectPath
	out, err := cmd.CombinedOutput()
	if err != nil {
		com.ExitOnFailure(err.Error(), 1)
	}
	return string(out)
}

func normalizeExecuteFileName(p buildParam, singleFileMode bool) {
	if singleFileMode {
		if len(p.Extension) > 0 {
			name := p.Executor + `-` + p.goos + `-` + p.goarch
			com.Rename(filepath.Join(p.ReleaseDir, name), filepath.Join(p.ReleaseDir, name+p.Extension))
		}
		return
	}
	files, err := filepath.Glob(filepath.Join(p.ReleaseDir, p.Executor+`-`+p.goos+`*`))
	if err != nil {
		com.ExitOnFailure(err.Error(), 1)
	}
	for _, file := range files {
		com.Rename(file, filepath.Join(p.ReleaseDir, p.Executor+p.Extension))
		break
	}
}

func packFiles(p buildParam) {
	var files []string
	var err error
	for _, copyFile := range p.CopyFiles {
		f := filepath.Join(p.ProjectPath, copyFile)
		if strings.Contains(f, `*`) {
			files, err = filepath.Glob(f)
			if err != nil {
				com.ExitOnFailure(err.Error(), 1)
			}
			for _, file := range files {
				destFile := filepath.Join(p.ReleaseDir, strings.TrimPrefix(file, p.ProjectPath))
				com.MkdirAll(filepath.Dir(destFile), os.ModePerm)
				err = com.Copy(file, destFile)
				if err != nil {
					com.ExitOnFailure(err.Error(), 1)
				}
			}
			continue
		}
		if com.IsDir(f) {
			err = com.CopyDir(f, filepath.Join(p.ReleaseDir, copyFile))
			if err != nil {
				com.ExitOnFailure(err.Error(), 1)
			}
			continue
		}
		destFile := filepath.Join(p.ReleaseDir, copyFile)
		com.MkdirAll(filepath.Dir(destFile), os.ModePerm)
		err = com.Copy(f, destFile)
		if err != nil {
			com.ExitOnFailure(err.Error(), 1)
		}
	}
	for _, newDir := range p.MakeDirs {
		err = com.MkdirAll(filepath.Join(p.ReleaseDir, newDir), os.ModePerm)
		if err != nil {
			com.ExitOnFailure(err.Error(), 1)
		}
	}
	err = com.TarGz(p.ReleaseDir, p.ReleaseDir+`.tar.gz`)
	if err != nil {
		com.ExitOnFailure(err.Error(), 1)
	}
	err = os.RemoveAll(p.ReleaseDir)
	if err != nil {
		com.ExitOnFailure(err.Error(), 1)
	}
	// 解压: tar -zxvf nging_linux_amd64.tar.gz -C ./nging_linux_amd64
}

func genComment(vendorMiscDirs ...string) string {
	comment := "//go:generate go install github.com/admpub/bindata/v3/go-bindata@latest\n"
	comment += `//go:generate go-bindata -fs -o bindata_assetfs.go -ignore "\\.(git|svn|DS_Store|less|scss|gitkeep)$" -minify "\\.(js|css)$" -tags bindata`
	prefixes := []string{}
	miscDirs := []string{`public/assets/`, `template/`, `config/i18n/`}
	miscDirs = append(miscDirs, vendorMiscDirs...)
	uniquePrefixes := map[string]struct{}{}
	for k, v := range miscDirs {
		if !strings.HasSuffix(v, `/...`) {
			if !strings.HasSuffix(v, `/`) {
				v += `/`
			}
			v += `...`
		}
		if strings.HasPrefix(v, `vendor/`) {
			parts := strings.SplitN(v, `/`, 5)
			if len(parts) == 5 { // `vendor/github.com/nging-plugins/collector/template/`  `vendor/github.com/nging-plugins/collector/public/`
				prefix := strings.Join(parts[0:4], `/`) + `/`
				if _, ok := uniquePrefixes[prefix]; !ok {
					uniquePrefixes[prefix] = struct{}{}
					prefixes = append(prefixes, prefix)
				}
			}
		}
		miscDirs[k] = v
	}
	comment += ` -prefix "` + strings.Join(prefixes, `|`) + `" `
	comment += strings.Join(miscDirs, ` `)
	return comment
}

func makeGenerateCommandComment(c Config) {
	dfts := p.VendorMiscDirs[`*`]
	for osName, miscDirs := range p.VendorMiscDirs {
		if osName == `*` {
			continue
		}
		dirs := make([]string, 0, len(dfts)+len(miscDirs))
		dirs = append(dirs, dfts...)
		dirs = append(dirs, miscDirs...)
		fileName := `main_`
		if strings.HasPrefix(osName, `!`) {
			fileName += `non` + strings.TrimPrefix(osName, `!`)
		} else {
			fileName += osName
		}
		fileName += `.go`
		filePath := filepath.Join(p.ProjectPath, fileName)
		fileContent := "//go:build " + osName + "\n\n"
		fileContent += "package main\n\n"
		fileContent += genComment(dirs...) + "\n\n"
		b, err := os.ReadFile(filePath)
		if err == nil {
			old := string(b)
			pos := strings.Index(old, `import `)
			if pos > -1 {
				fileContent += old[pos:]
			}
		}
		err = os.WriteFile(filePath, []byte(fileContent), os.ModePerm)
		if err != nil {
			fmt.Println(err.Error())
		}
	}
}

type Config struct {
	GoVersion      string
	GoImage        string
	GoProxy        string
	Executor       string
	NgingVersion   string
	NgingLabel     string
	Project        string
	VendorMiscDirs map[string][]string // key: GOOS
	BuildTags      []string
	CopyFiles      []string
	MakeDirs       []string
	Compiler       string
	CgoEnabled     bool
}

func (a Config) apply() {
	if len(a.GoVersion) > 0 {
		p.GoVersion = a.GoVersion
	}
	if len(a.Executor) > 0 {
		p.Executor = a.Executor
	}
	if len(a.NgingVersion) > 0 {
		p.NgingVersion = a.NgingVersion
	}
	if len(a.NgingLabel) > 0 {
		p.NgingLabel = a.NgingLabel
	}
	if len(a.Project) > 0 {
		p.Project = a.Project
	}
	p.GoImage = a.GoImage
	p.BuildTags = a.BuildTags
	p.CopyFiles = a.CopyFiles
	p.MakeDirs = a.MakeDirs
	p.Compiler = a.Compiler
	p.CgoEnabled = a.CgoEnabled
	p.GoProxy = a.GoProxy
}
