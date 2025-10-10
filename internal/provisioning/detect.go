package provisioning

import (
    "os"
    "path/filepath"
    "runtime"
    "strings"
)

// DetectProvider attempts to infer the current virtualization/cloud provider.
// It returns a normalized provider name ("vmware", "libvirt", "aws", "azure", "gcp", "unknown")
// and optional metadata extracted from local environment hints.
func DetectProvider() (string, map[string]string) {
    // Environment-driven hints first
    if v := strings.TrimSpace(os.Getenv("AWS_REGION")); v != "" || strings.TrimSpace(os.Getenv("AWS_DEFAULT_REGION")) != "" {
        md := map[string]string{}
        if v == "" {
            v = strings.TrimSpace(os.Getenv("AWS_DEFAULT_REGION"))
        }
        if v != "" {
            md["region"] = v
        }
        return "aws", md
    }
    if sub := strings.TrimSpace(os.Getenv("AZURE_SUBSCRIPTION_ID")); sub != "" {
        return "azure", map[string]string{"subscription_id": sub}
    }

    // Linux DMI hints
    if runtime.GOOS == "linux" {
        if val := readFirst("/sys/class/dmi/id/sys_vendor"); strings.Contains(strings.ToLower(val), "vmware") {
            return "vmware", map[string]string{}
        }
        if val := readFirst("/sys/class/dmi/id/product_name"); val != "" {
            low := strings.ToLower(val)
            switch {
            case strings.Contains(low, "kvm") || strings.Contains(low, "qemu"):
                return "libvirt", map[string]string{}
            case strings.Contains(low, "amazonec2") || strings.Contains(low, "amazon ec2"):
                return "aws", map[string]string{}
            case strings.Contains(low, "google compute engine") || strings.Contains(low, "google"):
                return "gcp", map[string]string{}
            case strings.Contains(low, "microsoft corporation") || exists("/var/lib/waagent"):
                return "azure", map[string]string{}
            }
        }
        if uuid := readFirst("/sys/hypervisor/uuid"); strings.HasPrefix(strings.ToLower(uuid), "ec2") {
            return "aws", map[string]string{}
        }
    }

    // Azure Linux Agent directory
    if exists("/var/lib/waagent") {
        return "azure", map[string]string{}
    }

    return "unknown", map[string]string{}
}

func readFirst(path string) string {
    b, err := os.ReadFile(filepath.Clean(path))
    if err != nil {
        return ""
    }
    return strings.TrimSpace(string(b))
}

func exists(path string) bool {
    if path == "" {
        return false
    }
    _, err := os.Stat(path)
    return err == nil
}
