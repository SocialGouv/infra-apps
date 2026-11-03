package chart_test

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"
)

type container struct {
	Name, Image  string
	Command      []string
	Env          []struct{ Name, Value string }
	VolumeMounts []struct {
		Name, MountPath string
		ReadOnly        bool
	}
}

type object struct {
	Raw      map[string]any `json:"-"`
	Kind     string
	Metadata struct {
		Name, Namespace string
		Labels          map[string]string
	}
	Data map[string]string
	Spec struct {
		MaxUnavailable json.RawMessage
		Selector       struct{ MatchLabels map[string]string }
		Template       struct {
			Metadata struct{ Labels, Annotations map[string]string }
			Spec     struct {
				InitContainers, Containers []container
				Volumes                    []struct {
					Name      string
					ConfigMap struct{ Name string }
					EmptyDir  *struct{}
				}
			}
		}
	}
}

func repoRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "iterion", "Chart.yaml")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("cannot locate infra-apps from CWD")
		}
		dir = parent
	}
}

func render(t *testing.T, cluster string, overrides ...string) map[string]object {
	t.Helper()
	return renderRelease(t, "iterion", cluster, overrides...)
}

func renderRelease(t *testing.T, release, cluster string, overrides ...string) map[string]object {
	t.Helper()
	root := repoRoot(t)
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	args := []string{"template", release, filepath.Join(root, "iterion"), "-n", "iterion", "-f", filepath.Join(root, "iterion", "values."+cluster+".yaml"), "--set", "cluster=" + cluster}
	for _, value := range overrides {
		args = append(args, "--set", value)
	}
	cmd := exec.CommandContext(ctx, "helm", args...)
	var diagnostics bytes.Buffer
	cmd.Stderr = &diagnostics
	yaml, err := cmd.Output()
	if err != nil {
		t.Fatalf("helm render: %v: %s", err, &diagnostics)
	}
	convert := exec.CommandContext(ctx, "yq", "-o=json", "-I=0", ".")
	convert.Stdin = bytes.NewReader(yaml)
	data, err := convert.Output()
	if err != nil {
		t.Fatal(err)
	}
	result := make(map[string]object)
	decoder := json.NewDecoder(bytes.NewReader(data))
	for {
		var raw json.RawMessage
		if err := decoder.Decode(&raw); err == io.EOF {
			break
		} else if err != nil {
			t.Fatal(err)
		}
		var o object
		if err := json.Unmarshal(raw, &o); err != nil {
			t.Fatal(err)
		}
		if err := json.Unmarshal(raw, &o.Raw); err != nil {
			t.Fatal(err)
		}
		if o.Kind != "" {
			result[o.Kind+"/"+o.Metadata.Name] = o
		}
	}
	return result
}

func TestValkeyHAPDBFollowsWorkloadNames(t *testing.T) {
	for _, tc := range []struct {
		name, release string
		values        []string
	}{
		{name: "default", release: "iterion"},
		{name: "name", release: "iterion", values: []string{"valkeyha.nameOverride=cache-ha"}},
		{name: "full-name", release: "iterion", values: []string{"valkeyha.fullnameOverride=cache-ha"}},
		{name: "long-name", release: "iterion", values: []string{"valkeyha.nameOverride=" + strings.Repeat("cache-", 14)}},
		{name: "release", release: "fixture-release"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			objects := renderRelease(t, tc.release, "ovh-prod", tc.values...)
			var workload object
			for _, o := range objects {
				if o.Kind != "StatefulSet" {
					continue
				}
				for _, c := range o.Spec.Template.Spec.InitContainers {
					// Both Valkey dependencies have an init container. Only
					// this workload carries the patched discovery volume.
					for _, mount := range c.VolumeMounts {
						if mount.Name != "master-discovery" {
							continue
						}
						if workload.Kind != "" {
							t.Fatal("ambiguous Valkey HA workload")
						}
						workload = o
					}
				}
			}
			pdb, ok := objects["PodDisruptionBudget/"+workload.Metadata.Name]
			if workload.Kind == "" || !ok || len(pdb.Spec.Selector.MatchLabels) != 2 {
				t.Fatalf("PDB does not follow workload %q", workload.Metadata.Name)
			}
			var matched []string
			for _, o := range objects {
				if o.Kind != "StatefulSet" {
					continue
				}
				matches := true
				for key, value := range pdb.Spec.Selector.MatchLabels {
					matches = matches && o.Spec.Template.Metadata.Labels[key] == value
				}
				if matches {
					matched = append(matched, o.Metadata.Name)
				}
			}
			if !reflect.DeepEqual(matched, []string{workload.Metadata.Name}) {
				t.Fatalf("PDB selects %v, want only %s", matched, workload.Metadata.Name)
			}
		})
	}
}

func chartEntries(t *testing.T, path string) map[string]string {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	gz, err := gzip.NewReader(f)
	if err != nil {
		t.Fatal(err)
	}
	defer gz.Close()
	tr := tar.NewReader(gz)
	entries := make(map[string]string)
	for {
		header, err := tr.Next()
		if err == io.EOF {
			return entries
		}
		if err != nil {
			t.Fatal(err)
		}
		if header.Typeflag == tar.TypeDir {
			continue
		}
		if header.Typeflag != tar.TypeReg {
			t.Fatalf("unexpected archive entry %s type %d", header.Name, header.Typeflag)
		}
		if _, duplicate := entries[header.Name]; duplicate {
			t.Fatalf("duplicate archive entry %s", header.Name)
		}
		data, err := io.ReadAll(tr)
		if err != nil {
			t.Fatal(err)
		}
		entries[header.Name] = string(data)
	}
}

func TestValkeyHAPackagedChartMatchesEverySource(t *testing.T) {
	root := repoRoot(t)
	target := t.TempDir()
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, "helm", "package", filepath.Join(root, "iterion/vendor-charts/valkeyha"), "--destination", target)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("package chart sources: %v: %s", err, out)
	}
	want := chartEntries(t, filepath.Join(target, "valkeyha-0.10.3.tgz"))
	got := chartEntries(t, filepath.Join(root, "iterion/charts/valkeyha-0.10.3.tgz"))
	for name, data := range want {
		if content, ok := got[name]; !ok || content != data {
			t.Errorf("packaged chart differs from source: %s; run helm package", name)
		}
	}
	for name := range got {
		if _, ok := want[name]; !ok {
			t.Errorf("packaged chart retains removed source: %s; run helm package", name)
		}
	}
}

func TestValkeyHAChartCarriesThePatchAndOnlyItsOwnPDB(t *testing.T) {
	objects := render(t, "ovh-prod")
	pod := objects["StatefulSet/iterion-valkeyha"].Spec.Template
	pdb, found := objects["PodDisruptionBudget/iterion-valkeyha"]
	if !found || string(pdb.Spec.MaxUnavailable) != "1" || pdb.Metadata.Namespace != "iterion" {
		t.Fatalf("missing exact valkeyha PDB: %+v", pdb)
	}
	if len(pdb.Spec.Selector.MatchLabels) != 2 {
		t.Fatal("PDB selector does not identify exactly the valkeyha workload")
	}
	for key, value := range pdb.Spec.Selector.MatchLabels {
		if pod.Metadata.Labels[key] != value {
			t.Fatalf("PDB selector %s=%s misses its pods", key, value)
		}
	}
	if pdb.Spec.Selector.MatchLabels["app.kubernetes.io/name"] != "valkeyha" {
		t.Fatal("PDB targets the other Valkey instance")
	}
	helper := objects["ConfigMap/iterion-valkeyha-master-discovery"].Data["discover-master.sh"]
	source, err := os.ReadFile(filepath.Join(repoRoot(t), "iterion/vendor-charts/valkeyha/scripts/discover-master.sh"))
	if err != nil {
		t.Fatal(err)
	}
	if helper != string(source) {
		t.Fatal("packaged chart carries a stale master discovery script; run helm package")
	}
	digest := sha256.Sum256(source)
	if pod.Metadata.Annotations["checksum/master-discovery"] != hex.EncodeToString(digest[:]) {
		t.Fatal("script changes do not trigger a pod rollout")
	}
	for _, c := range readers(t, objects) {
		mounts := make(map[string]string)
		for _, m := range c.VolumeMounts {
			mounts[m.Name] = m.MountPath
		}
		if mounts["master-discovery"] != "/discovery" || mounts["valkey-config"] != "/tmp/valkey" {
			t.Fatalf("%s does not share the script and remembered state", c.Name)
		}
	}
	for _, o := range render(t, "ovh-dev") {
		if o.Metadata.Name == "iterion-valkeyha" || o.Metadata.Name == "iterion-valkeyha-master-discovery" {
			t.Fatal("prod-only valkeyha leaked into dev")
		}
	}
	for key, o := range render(t, "ovh-prod", "valkeyha.enabled=false") {
		if o.Metadata.Name == "iterion-valkeyha" || o.Metadata.Name == "iterion-valkeyha-master-discovery" {
			t.Fatal("disabled valkeyha still has resources")
		}
		if strings.HasPrefix(o.Metadata.Name, "iterion-valkeyfiler") && !reflect.DeepEqual(o.Raw, objects[key].Raw) {
			t.Fatalf("enabling valkeyha changes the separate valkeyfiler resource %s", key)
		}
	}
}

func readers(t *testing.T, objects map[string]object) []container {
	t.Helper()
	pod := objects["StatefulSet/iterion-valkeyha"].Spec.Template.Spec
	var result []container
	for _, c := range append(pod.InitContainers, pod.Containers...) {
		if c.Name == "valkey-init" || c.Name == "sentinel" {
			result = append(result, c)
		}
	}
	if len(result) != 2 {
		t.Fatalf("bootstrap readers = %d, want both init and sentinel", len(result))
	}
	return result
}

func TestValkeyHABothRenderedReadersRejectAStaleMaster(t *testing.T) {
	objects := render(t, "ovh-prod", "valkeyha.sentinel.masterDiscovery.maxAttempts=4")
	for _, tc := range []struct {
		name, want                                  string
		ordinal                                     int
		fails, remembered, unlimited, authenticated bool
	}{
		{name: "stale-first", ordinal: 2, want: "10.1.0.20 6382"},
		{name: "self-first", ordinal: 2, want: "10.1.0.20 6382"},
		{name: "transition", ordinal: 2, want: "10.1.0.20 6382"},
		{name: "transition-unlimited", ordinal: 2, want: "10.1.0.20 6382", unlimited: true},
		{name: "stale-only", ordinal: 2, fails: true},
		{name: "replica-only", ordinal: 2, fails: true},
		{name: "auth-error", ordinal: 0, fails: true},
		{name: "authenticated", ordinal: 2, want: "10.1.0.20 6382", authenticated: true},
		{name: "invalid-port", ordinal: 0, fails: true},
		{name: "zero-port", ordinal: 0, fails: true},
		{name: "large-port", ordinal: 0, fails: true},
		{name: "host-space", ordinal: 0, fails: true},
		{name: "extra-line", ordinal: 0, fails: true},
		{name: "bootstrap", ordinal: 0},
		{name: "bootstrap-replica", ordinal: 2, want: "iterion-valkeyha-0.iterion-valkeyha-headless.iterion.svc.cluster.local 6379"},
		{name: "remembered-silence", ordinal: 0, fails: true, remembered: true},
		{name: "self-only", ordinal: 2},
	} {
		caseObjects := objects
		if tc.authenticated {
			caseObjects = render(t, "ovh-prod", "valkeyha.sentinel.masterDiscovery.maxAttempts=4", "valkeyha.auth.enabled=true", "valkeyha.auth.password=fixture-only")
		}
		for _, reader := range readers(t, caseObjects) {
			t.Run(tc.name+"/"+reader.Name, func(t *testing.T) {
				dir := t.TempDir()
				bin, config := filepath.Join(dir, "bin"), filepath.Join(dir, "config")
				for _, path := range []string{bin, config} {
					if err := os.Mkdir(path, 0700); err != nil {
						t.Fatal(err)
					}
				}
				helper := filepath.Join(dir, "discover-master.sh")
				write(t, helper, objects["ConfigMap/iterion-valkeyha-master-discovery"].Data["discover-master.sh"], 0600)
				executable, err := os.Executable()
				if err != nil {
					t.Fatal(err)
				}
				for _, name := range []string{"valkey-cli", "hostname", "sleep", "valkey-sentinel", "getent"} {
					write(t, filepath.Join(bin, name), "#!/bin/sh\nexec "+shellQuote(executable)+" -test.run='^TestValkeyFakeCommand$' -- "+name+" \"$@\"\n", 0700)
				}
				state := filepath.Join(config, "sentinel-seen")
				if tc.remembered {
					write(t, state, "", 0600)
				}
				if tc.name == "self-only" && reader.Name == "valkey-init" {
					write(t, filepath.Join(config, "valkey.conf"), "replicaof 10.1.0.99 6379\n", 0600)
				}
				body := strings.ReplaceAll(reader.Command[2], "/tmp/valkey", config)
				body = strings.ReplaceAll(body, "/tmp/sentinel.conf", filepath.Join(dir, "sentinel.conf"))
				body = strings.ReplaceAll(body, "/discovery/discover-master.sh", helper)
				body = strings.ReplaceAll(body, "/etc/valkey", filepath.Join(dir, "absent-config"))
				ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
				defer cancel()
				cmd := exec.CommandContext(ctx, "sh", "-c", body)
				cmd.Dir = dir
				cmd.Env = append(os.Environ(), "PATH="+bin+string(os.PathListSeparator)+os.Getenv("PATH"), "VALKEY_FIXTURE_DIR="+dir, "VALKEY_FIXTURE_SCENARIO="+tc.name, "VALKEY_FIXTURE_ORDINAL="+strconv.Itoa(tc.ordinal))
				for _, e := range reader.Env {
					value := strings.ReplaceAll(e.Value, "/tmp/valkey", config)
					if e.Name == "VALKEY_PASSWORD" {
						value = "fixture-only"
					}
					if tc.unlimited && (e.Name == "VALKEY_DISCOVERY_MAX_ATTEMPTS" || e.Name == "VALKEY_DISCOVERY_COMMAND_TIMEOUT") {
						value = "0"
					}
					cmd.Env = append(cmd.Env, e.Name+"="+value)
				}
				out, runErr := cmd.CombinedOutput()
				if ctx.Err() != nil {
					t.Fatalf("fixture hung: %v: %s", ctx.Err(), out)
				}
				path, prefix := filepath.Join(config, "valkey.conf"), "replicaof "
				if reader.Name == "sentinel" {
					path, prefix = filepath.Join(dir, "sentinel.conf"), "sentinel monitor mymaster "
				}
				data, _ := os.ReadFile(path)
				var selected string
				for _, line := range strings.Split(string(data), "\n") {
					if strings.HasPrefix(line, prefix) {
						selected = strings.TrimPrefix(line, prefix)
						if reader.Name == "sentinel" {
							selected = strings.TrimSuffix(selected, " 2")
						}
					}
				}
				if tc.fails {
					if runErr == nil || selected != "" {
						t.Fatalf("unsafe master accepted: selected=%q err=%v output=%s", selected, runErr, out)
					}
					if !bytes.Contains(out, []byte("refusing bootstrap")) {
						t.Fatalf("failure did not name its refusal: %v %s", runErr, out)
					}
					return
				}
				want := tc.want
				if want == "" && reader.Name == "sentinel" {
					want = "10.1.0.10 6379"
					if tc.name == "bootstrap" {
						want = "iterion-valkeyha-0.iterion-valkeyha-headless.iterion.svc.cluster.local 6379"
					}
				}
				if runErr != nil || selected != want {
					t.Fatalf("selected=%q want=%q err=%v output=%s", selected, want, runErr, out)
				}
				if strings.HasPrefix(tc.name, "transition") {
					round, _ := os.ReadFile(filepath.Join(dir, "round"))
					if string(round) != "2" {
						t.Fatalf("silent retry reset bootstrap knowledge: rounds=%q", round)
					}
				}
			})
		}
	}
}

func write(t *testing.T, path, data string, mode os.FileMode) {
	t.Helper()
	if err := os.WriteFile(path, []byte(data), mode); err != nil {
		t.Fatal(err)
	}
}

func shellQuote(s string) string { return "'" + strings.ReplaceAll(s, "'", "'\"'\"'") + "'" }

// This subprocess is the wire peer of the real rendered shell scripts. It
// changes Sentinel answers by completed retry, never by wall-clock timing.
func TestValkeyFakeCommand(t *testing.T) {
	dir := os.Getenv("VALKEY_FIXTURE_DIR")
	if dir == "" {
		return
	}
	args := os.Args
	for len(args) > 0 && args[0] != "--" {
		args = args[1:]
	}
	if len(args) < 2 {
		os.Exit(2)
	}
	name, args := args[1], args[2:]
	ordinal := os.Getenv("VALKEY_FIXTURE_ORDINAL")
	scenario := os.Getenv("VALKEY_FIXTURE_SCENARIO")
	roundBytes, _ := os.ReadFile(filepath.Join(dir, "round"))
	round, _ := strconv.Atoi(string(roundBytes))
	switch name {
	case "hostname":
		if len(args) > 0 && args[0] == "-i" {
			fmt.Println("10.1.0.10")
		} else {
			fmt.Println("iterion-valkeyha-" + ordinal)
		}
	case "getent":
		os.Exit(1)
	case "valkey-sentinel":
	case "sleep":
		if err := os.WriteFile(filepath.Join(dir, "round"), []byte(strconv.Itoa(round+1)), 0600); err != nil {
			os.Exit(2)
		}
	case "valkey-cli":
		host, port, operation := "", "", ""
		for i, arg := range args {
			if arg == "-h" && i+1 < len(args) {
				host = args[i+1]
			}
			if arg == "-p" && i+1 < len(args) {
				port = args[i+1]
			}
			switch strings.ToLower(arg) {
			case "ping", "role", "get-master-addr-by-name":
				operation = strings.ToLower(arg)
			}
		}
		if operation == "ping" {
			fmt.Println("PONG")
			os.Exit(0)
		}
		if scenario == "authenticated" && os.Getenv("VALKEYCLI_AUTH") != "fixture-only" {
			fmt.Println("NOAUTH Authentication required.")
			os.Exit(0)
		}
		if operation == "get-master-addr-by-name" {
			if strings.HasPrefix(scenario, "bootstrap") || scenario == "remembered-silence" || (strings.HasPrefix(scenario, "transition") && round == 1) {
				os.Exit(1)
			}
			if scenario == "auth-error" {
				fmt.Println("NOAUTH Authentication required.")
				os.Exit(0)
			}
			if scenario == "invalid-port" {
				fmt.Println("10.1.0.20\nnot-a-port")
				os.Exit(0)
			}
			if scenario == "zero-port" {
				fmt.Println("10.1.0.20\n0")
				os.Exit(0)
			}
			if scenario == "large-port" {
				fmt.Println("10.1.0.20\n65536")
				os.Exit(0)
			}
			if scenario == "host-space" {
				fmt.Println("invalid host\n6382")
				os.Exit(0)
			}
			if scenario == "extra-line" {
				fmt.Println("10.1.0.20\n6382\nextra")
				os.Exit(0)
			}
			if scenario == "self-only" || (scenario == "self-first" && strings.HasPrefix(host, "iterion-valkeyha-0.")) {
				fmt.Println("10.1.0.10\n6379")
				os.Exit(0)
			}
			if scenario == "stale-only" || (scenario == "stale-first" && strings.HasPrefix(host, "iterion-valkeyha-0.")) || (strings.HasPrefix(scenario, "transition") && round == 0) {
				fmt.Println("10.1.0.99\n6381")
				os.Exit(0)
			}
			fmt.Println("10.1.0.20\n6382")
		} else if operation == "role" {
			if scenario == "replica-only" {
				fmt.Println("slave\n10.1.0.99\n6379\nconnected\n0")
				os.Exit(0)
			}
			if (host == "10.1.0.20" && port == "6382") || (host == "10.1.0.10" && port == "6379") || (strings.HasPrefix(scenario, "bootstrap") && strings.HasPrefix(host, "iterion-valkeyha-0.")) {
				fmt.Print("master\n0\n")
				os.Exit(0)
			}
			os.Exit(1)
		} else {
			os.Exit(2)
		}
	default:
		os.Exit(2)
	}
	os.Exit(0)
}
