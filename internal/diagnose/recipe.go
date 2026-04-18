package diagnose

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

type RecipeName string

const (
	RecipeDiskUsage           RecipeName = "disk_usage"
	RecipeDiskInodes          RecipeName = "disk_inodes"
	RecipeMemoryPressure      RecipeName = "memory_pressure"
	RecipePerformanceCPU      RecipeName = "performance_cpu"
	RecipeDNSResolution       RecipeName = "dns_resolution"
	RecipeDNSHosts            RecipeName = "dns_hosts"
	RecipeNetworkConnectivity RecipeName = "network_connectivity"
	RecipeServiceFailure      RecipeName = "service_failure"
	RecipeDockerCrash         RecipeName = "docker_crash"
	RecipeBuildFailure        RecipeName = "build_failure"
	RecipePermission          RecipeName = "permission"
	RecipePermissionMount     RecipeName = "permission_mount"
	RecipePortConflict        RecipeName = "port_conflict"
	RecipeSSL                 RecipeName = "ssl"
	RecipeGit                 RecipeName = "git"
	RecipeCron                RecipeName = "cron"
	RecipePackage             RecipeName = "package"
	RecipeProcess             RecipeName = "process"
	RecipeSSH                 RecipeName = "ssh"
	RecipeTime                RecipeName = "time_sync"
	RecipeLog                 RecipeName = "log_management"
	RecipeDatabase            RecipeName = "database"
	RecipeFirewall            RecipeName = "firewall"
	RecipeUser                RecipeName = "user_account"
	RecipeIO                  RecipeName = "disk_io"
	RecipeHardware            RecipeName = "hardware"
	RecipeBoot                RecipeName = "boot"
	RecipeNFS                 RecipeName = "nfs_mount"
)

type Recipe struct {
	Name           RecipeName
	IssueClass     IssueClass
	InitialCommand string
	ServiceName    string
}

func SelectRecipe(userInput string) *Recipe {
	if looksLikeKnowledgeQuery(userInput) {
		return nil
	}

	issueClass := ClassifyIssue(userInput)
	serviceName := ExtractServiceName(strings.ToLower(userInput))

	switch issueClass {
	case IssueDisk:
		normalized := strings.ToLower(userInput)
		if containsAny(normalized, "inode", "can't create", "cannot create", "no space left") &&
			containsAny(normalized, "space available", "shows space", "plenty of space", "df shows") {
			return &Recipe{Name: RecipeDiskInodes, IssueClass: issueClass, InitialCommand: "df -i"}
		}
		return &Recipe{
			Name:           RecipeDiskUsage,
			IssueClass:     issueClass,
			InitialCommand: "df -h",
		}
	case IssueMemory:
		cmd := "free -h"
		if osPlatform == "darwin" {
			cmd = "vm_stat"
		}
		return &Recipe{
			Name:           RecipeMemoryPressure,
			IssueClass:     issueClass,
			InitialCommand: cmd,
		}
	case IssuePerformance:
		return &Recipe{
			Name:           RecipePerformanceCPU,
			IssueClass:     issueClass,
			InitialCommand: "uptime",
		}
	case IssueDNS:
		normalized := strings.ToLower(userInput)
		host := ExtractHostname(normalized)
		if containsAny(normalized, "wrong ip", "wrong address", "resolves to") && host != "" {
			return &Recipe{
				Name:           RecipeDNSHosts,
				IssueClass:     issueClass,
				InitialCommand: fmt.Sprintf("grep %s /etc/hosts; cat /etc/resolv.conf", shellEscapeToken(host)),
			}
		}
		cmd := "cat /etc/resolv.conf"
		if osPlatform == "darwin" {
			cmd = "scutil --dns"
		}
		return &Recipe{
			Name:           RecipeDNSResolution,
			IssueClass:     issueClass,
			InitialCommand: cmd,
		}
	case IssueNetwork:
		cmd := "ip -o addr show"
		if osPlatform == "darwin" {
			cmd = "ifconfig"
		}
		return &Recipe{
			Name:           RecipeNetworkConnectivity,
			IssueClass:     issueClass,
			InitialCommand: cmd,
		}
	case IssueDocker:
		containerName := ExtractContainerName(strings.ToLower(userInput))
		cmd := "docker ps -a --format 'table {{.Names}}\\t{{.Status}}\\t{{.Image}}' | head -20"
		if containerName != "" {
			cmd = fmt.Sprintf("docker inspect --format '{{.State.Status}} exit:{{.State.ExitCode}} oom:{{.State.OOMKilled}}' %s", shellEscapeToken(containerName))
		}
		return &Recipe{
			Name:           RecipeDockerCrash,
			IssueClass:     issueClass,
			InitialCommand: cmd,
			ServiceName:    containerName,
		}
	case IssueBuild:
		buildTool := ExtractBuildTool(strings.ToLower(userInput))
		cmd := buildCommandForTool(buildTool)
		return &Recipe{
			Name:           RecipeBuildFailure,
			IssueClass:     issueClass,
			InitialCommand: cmd,
			ServiceName:    buildTool,
		}
	case IssueService:
		cmd := "systemctl --failed --no-pager --plain 2>/dev/null || service --status-all 2>/dev/null || grep -iE 'error|fatal|fail' /var/log/syslog /var/log/messages 2>/dev/null | tail -20"
		if osPlatform == "darwin" {
			cmd = "launchctl list | head -50"
		}
		if serviceName != "" {
			if osPlatform == "darwin" {
				cmd = fmt.Sprintf("launchctl list | grep -i %s", shellEscapeToken(serviceName))
			} else {
				cmd = fmt.Sprintf("systemctl status %s --no-pager --full -n 20 2>/dev/null || service %s status 2>/dev/null || grep -i %s /var/log/syslog /var/log/messages 2>/dev/null | tail -20",
					shellEscapeToken(serviceName), shellEscapeToken(serviceName), shellEscapeToken(serviceName))
			}
		}
		return &Recipe{
			Name:           RecipeServiceFailure,
			IssueClass:     issueClass,
			InitialCommand: cmd,
			ServiceName:    serviceName,
		}
	case IssuePermission:
		path := ExtractPath(strings.ToLower(userInput))
		normalized := strings.ToLower(userInput)
		if containsAny(normalized, "noexec") ||
			(containsAny(normalized, "permission") && containsAny(normalized, "755", "executable", "script") && containsAny(normalized, "still", "denied")) {
			cmd := "mount | grep noexec"
			if path != "" {
				cmd = fmt.Sprintf("ls -la %s; mount | grep noexec", shellEscapeToken(path))
			}
			return &Recipe{
				Name:           RecipePermissionMount,
				IssueClass:     issueClass,
				InitialCommand: cmd,
				ServiceName:    path,
			}
		}
		cmd := "id"
		if path != "" {
			cmd = fmt.Sprintf("ls -la %s; id", shellEscapeToken(path))
		}
		return &Recipe{
			Name:           RecipePermission,
			IssueClass:     issueClass,
			InitialCommand: cmd,
			ServiceName:    path,
		}
	case IssuePort:
		cmd := "ss -tlnp"
		if osPlatform == "darwin" {
			cmd = "lsof -iTCP -sTCP:LISTEN -P -n"
		}
		port := ExtractPort(strings.ToLower(userInput))
		return &Recipe{
			Name:           RecipePortConflict,
			IssueClass:     issueClass,
			InitialCommand: cmd,
			ServiceName:    port,
		}
	case IssueSSL:
		cmd := "openssl s_client -connect localhost:443 </dev/null 2>/dev/null | openssl x509 -noout -dates -subject"
		return &Recipe{
			Name:           RecipeSSL,
			IssueClass:     issueClass,
			InitialCommand: cmd,
		}
	case IssueGit:
		return &Recipe{
			Name:           RecipeGit,
			IssueClass:     issueClass,
			InitialCommand: "git status",
		}
	case IssueCron:
		cmd := "crontab -l 2>&1"
		if osPlatform == "linux" {
			cmd = "crontab -l 2>&1; systemctl list-timers --no-pager 2>/dev/null | head -20"
		}
		return &Recipe{
			Name:           RecipeCron,
			IssueClass:     issueClass,
			InitialCommand: cmd,
		}
	case IssuePackage:
		cmd := buildPackageCommand()
		return &Recipe{
			Name:           RecipePackage,
			IssueClass:     issueClass,
			InitialCommand: cmd,
		}
	case IssueProcess:
		normalized := strings.ToLower(userInput)
		cmd := "ps aux | grep -E 'Z|defunct' | head -20"
		if containsAny(normalized, "open files", "ulimit", "file descriptor", "fd leak", "emfile", "enfile") {
			cmd = "ulimit -n; lsof 2>/dev/null | awk '{print $1}' | sort | uniq -c | sort -rn | head -10"
		}
		return &Recipe{
			Name:           RecipeProcess,
			IssueClass:     issueClass,
			InitialCommand: cmd,
		}
	case IssueSSH:
		return &Recipe{
			Name:           RecipeSSH,
			IssueClass:     issueClass,
			InitialCommand: "ls -la ~/.ssh/ 2>/dev/null; ssh-add -l 2>&1",
		}
	case IssueTime:
		cmd := "timedatectl status 2>/dev/null || date -u"
		if osPlatform == "darwin" {
			cmd = "sntp -d pool.ntp.org 2>&1 | head -5; date -u"
		}
		return &Recipe{
			Name:           RecipeTime,
			IssueClass:     issueClass,
			InitialCommand: cmd,
		}
	case IssueLog:
		cmd := "journalctl --disk-usage 2>/dev/null; du -sh /var/log/ 2>/dev/null"
		if osPlatform == "darwin" {
			cmd = "du -sh /var/log/ 2>/dev/null; ls -lhS /var/log/ 2>/dev/null | head -15"
		}
		return &Recipe{
			Name:           RecipeLog,
			IssueClass:     issueClass,
			InitialCommand: cmd,
		}
	case IssueDatabase:
		dbType := ExtractDatabaseType(strings.ToLower(userInput))
		cmd := buildDatabaseCommand(dbType)
		return &Recipe{
			Name:           RecipeDatabase,
			IssueClass:     issueClass,
			InitialCommand: cmd,
			ServiceName:    dbType,
		}
	case IssueFirewall:
		cmd := "iptables -L -n --line-numbers 2>/dev/null | head -40; nft list ruleset 2>/dev/null | head -40; ufw status verbose 2>/dev/null"
		if osPlatform == "darwin" {
			cmd = "pfctl -sr 2>/dev/null | head -30"
		}
		return &Recipe{
			Name:           RecipeFirewall,
			IssueClass:     issueClass,
			InitialCommand: cmd,
		}
	case IssueUser:
		user := ExtractUsername(strings.ToLower(userInput))
		cmd := "who; last -5"
		if user != "" {
			if osPlatform == "darwin" {
				cmd = fmt.Sprintf("id %s 2>/dev/null; dscl . -read /Users/%s 2>/dev/null | head -20", shellEscapeToken(user), shellEscapeToken(user))
			} else {
				cmd = fmt.Sprintf("id %s 2>/dev/null; passwd -S %s 2>/dev/null; grep %s /etc/passwd", shellEscapeToken(user), shellEscapeToken(user), shellEscapeToken(user))
			}
		}
		return &Recipe{
			Name:           RecipeUser,
			IssueClass:     issueClass,
			InitialCommand: cmd,
			ServiceName:    user,
		}
	case IssueIO:
		cmd := "iostat -xz 1 2 2>/dev/null | tail -20 || cat /proc/diskstats | head -20"
		if osPlatform == "darwin" {
			cmd = "iostat -c 2 2>/dev/null"
		}
		return &Recipe{
			Name:           RecipeIO,
			IssueClass:     issueClass,
			InitialCommand: cmd,
		}
	case IssueHardware:
		cmd := "dmesg -T 2>/dev/null | grep -iE 'error|fail|bad|sector|mce|temperature|thermal' | tail -20"
		if osPlatform == "darwin" {
			cmd = "system_profiler SPHardwareDataType 2>/dev/null; pmset -g thermlog 2>/dev/null | tail -10"
		}
		return &Recipe{
			Name:           RecipeHardware,
			IssueClass:     issueClass,
			InitialCommand: cmd,
		}
	case IssueBoot:
		cmd := "journalctl -xb --no-pager 2>/dev/null | tail -40 || dmesg -T | tail -40"
		if osPlatform == "darwin" {
			cmd = "log show --predicate 'process == \"kernel\"' --last 5m --style compact 2>/dev/null | tail -30"
		}
		return &Recipe{
			Name:           RecipeBoot,
			IssueClass:     issueClass,
			InitialCommand: cmd,
		}
	case IssueNFS:
		return &Recipe{
			Name:           RecipeNFS,
			IssueClass:     issueClass,
			InitialCommand: "mount -t nfs 2>/dev/null; mount -t nfs4 2>/dev/null; cat /etc/fstab 2>/dev/null | grep nfs",
		}
	default:
		return nil
	}
}

func (r *Recipe) FollowUpCommand(firstOutput string) string {
	if r == nil || strings.TrimSpace(firstOutput) == "" {
		return ""
	}

	switch r.Name {
	case RecipeDiskUsage:
		if !hasHighDiskUsage(firstOutput) {
			return ""
		}
		if osPlatform == "darwin" {
			return "du -xhd 1 /System/Volumes/Data 2>/dev/null | sort -hr | head -15"
		}
		return "du -xhd 1 / 2>/dev/null | sort -hr | head -15"
	case RecipeMemoryPressure:
		if osPlatform == "darwin" {
			return "ps -eo pid,rss,comm -r | head -10"
		}
		return "ps -eo pid,rss,comm --sort=-rss | head -10"
	case RecipePerformanceCPU:
		if osPlatform == "darwin" {
			return "ps -eo pid,pcpu,comm -r | head -10"
		}
		return "ps -eo pid,pcpu,comm --sort=-pcpu | head -10"
	case RecipeDNSResolution:
		if osPlatform == "darwin" {
			return "netstat -rn"
		}
		return "ip route"
	case RecipeNetworkConnectivity:
		if osPlatform == "darwin" {
			return "netstat -rn"
		}
		return "ip route"
	case RecipeDockerCrash:
		if r.ServiceName == "" {
			return ""
		}
		return fmt.Sprintf("docker logs --tail 40 %s 2>&1", shellEscapeToken(r.ServiceName))
	case RecipeBuildFailure:
		return ""
	case RecipePermission:
		path := r.ServiceName
		if path != "" {
			return fmt.Sprintf("stat %s", shellEscapeToken(path))
		}
		return ""
	case RecipePortConflict:
		port := r.ServiceName
		if port != "" {
			if osPlatform == "darwin" {
				return fmt.Sprintf("lsof -iTCP:%s -sTCP:LISTEN -P -n", shellEscapeToken(port))
			}
			return fmt.Sprintf("ss -tlnp | grep %s", shellEscapeToken(port))
		}
		return ""
	case RecipeSSL:
		return "date -u"
	case RecipeGit:
		return "git log --oneline -5"
	case RecipeCron:
		if osPlatform == "darwin" {
			return "log show --predicate 'process == \"cron\"' --last 10m --style compact 2>/dev/null | tail -20"
		}
		return "journalctl -u cron -n 20 --no-pager 2>/dev/null || journalctl -u crond -n 20 --no-pager 2>/dev/null"
	case RecipePackage:
		return ""
	case RecipeProcess:
		return "lsof 2>/dev/null | awk '{print $1}' | sort | uniq -c | sort -rn | head -10"
	case RecipeServiceFailure:
		if r.ServiceName == "" {
			return ""
		}
		if osPlatform == "darwin" {
			return fmt.Sprintf("log show --predicate 'process == \"%s\"' --last 5m --style compact 2>/dev/null | tail -40", r.ServiceName)
		}
		return fmt.Sprintf("journalctl -u %s -n 40 --no-pager 2>/dev/null || grep -i %s /var/log/syslog /var/log/messages 2>/dev/null | tail -20", shellEscapeToken(r.ServiceName), shellEscapeToken(r.ServiceName))
	case RecipeDiskInodes:
		return "find / -xdev -type d -exec sh -c 'echo \"$(find \"$1\" -maxdepth 1 | wc -l) $1\"' _ {} \\; 2>/dev/null | sort -rn | head -10"
	case RecipePermissionMount:
		return ""
	case RecipeDNSHosts:
		return ""
	case RecipeSSH:
		return "cat ~/.ssh/config 2>/dev/null | head -30"
	case RecipeTime:
		if osPlatform == "darwin" {
			return "systemsetup -getusingnetworktime 2>/dev/null; systemsetup -gettimezone 2>/dev/null"
		}
		return "chronyc tracking 2>/dev/null || ntpq -p 2>/dev/null"
	case RecipeLog:
		if osPlatform == "darwin" {
			return "ls -lhS /var/log/ | head -20"
		}
		return "journalctl -p err -n 30 --no-pager 2>/dev/null"
	case RecipeDatabase:
		return "journalctl -u postgresql -n 30 --no-pager 2>/dev/null || journalctl -u mysql -n 30 --no-pager 2>/dev/null"
	case RecipeFirewall:
		if osPlatform == "darwin" {
			return "lsof -iTCP -sTCP:LISTEN -P -n"
		}
		return "ss -tlnp"
	case RecipeUser:
		if r.ServiceName == "" {
			return ""
		}
		if osPlatform == "darwin" {
			return ""
		}
		return fmt.Sprintf("faillock --user %s 2>/dev/null; chage -l %s 2>/dev/null", shellEscapeToken(r.ServiceName), shellEscapeToken(r.ServiceName))
	case RecipeIO:
		if osPlatform == "darwin" {
			return ""
		}
		return "iotop -obn 1 2>/dev/null | head -15 || cat /proc/diskstats"
	case RecipeHardware:
		if osPlatform == "darwin" {
			return "diskutil info / 2>/dev/null | head -20"
		}
		return "smartctl -a /dev/sda 2>/dev/null | head -40 || sensors 2>/dev/null"
	case RecipeBoot:
		return "cat /etc/fstab 2>/dev/null; who -b; last reboot | head -5"
	case RecipeNFS:
		return "showmount -e localhost 2>/dev/null; nfsstat -c 2>/dev/null | head -20"
	default:
		return ""
	}
}

var pctRe = regexp.MustCompile(`(\d+)%`)

func hasHighDiskUsage(output string) bool {
	for _, match := range pctRe.FindAllStringSubmatch(output, -1) {
		if len(match) < 2 {
			continue
		}
		pct, err := strconv.Atoi(match[1])
		if err != nil {
			continue
		}
		if pct >= 85 {
			return true
		}
	}
	return false
}

func shellEscapeToken(value string) string {
	return "'" + strings.ReplaceAll(value, "'", `'"'"'`) + "'"
}

func buildCommandForTool(tool string) string {
	switch tool {
	case "npm":
		return "npm run build 2>&1 | tail -40"
	case "yarn":
		return "yarn build 2>&1 | tail -40"
	case "pnpm":
		return "pnpm run build 2>&1 | tail -40"
	case "cargo":
		return "cargo build 2>&1 | tail -40"
	case "go":
		return "go build ./... 2>&1 | tail -40"
	case "make":
		return "make 2>&1 | tail -40"
	case "tsc":
		return "tsc --noEmit 2>&1 | tail -40"
	default:
		return "ls package.json Cargo.toml go.mod Makefile 2>/dev/null"
	}
}

func buildDatabaseCommand(dbType string) string {
	switch dbType {
	case "postgres", "postgresql":
		return "pg_isready 2>/dev/null; systemctl status postgresql --no-pager 2>/dev/null | head -15 || service postgresql status 2>/dev/null"
	case "mysql", "mariadb":
		return "mysqladmin status 2>/dev/null; systemctl status mysql --no-pager 2>/dev/null | head -15 || service mysql status 2>/dev/null"
	case "redis":
		return "redis-cli ping 2>/dev/null; systemctl status redis --no-pager 2>/dev/null | head -15 || service redis status 2>/dev/null"
	default:
		if osPlatform == "darwin" {
			return "lsof -iTCP -sTCP:LISTEN -P -n | grep -E '5432|3306|6379|27017'"
		}
		return "ss -tlnp | grep -E '5432|3306|6379|27017' 2>/dev/null || lsof -iTCP -sTCP:LISTEN -P -n | grep -E '5432|3306|6379|27017'"
	}
}

func buildPackageCommand() string {
	if osPlatform == "darwin" {
		return "brew doctor 2>&1 | head -30"
	}
	return "apt list --upgradable 2>/dev/null | head -20 || dpkg --audit 2>/dev/null | head -20"
}

func looksLikeKnowledgeQuery(input string) bool {
	normalized := strings.ToLower(strings.TrimSpace(input))
	return strings.HasPrefix(normalized, "what is ") ||
		strings.HasPrefix(normalized, "what are ") ||
		strings.HasPrefix(normalized, "what does ") ||
		strings.HasPrefix(normalized, "explain ") ||
		strings.HasPrefix(normalized, "define ") ||
		strings.HasPrefix(normalized, "describe ")
}
