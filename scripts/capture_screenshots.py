#!/usr/bin/env python3
"""Capture docs/screenshots/*.png by running real tenantctl commands.

Every command listed below is actually executed (via subprocess) against the
real tenantctl binary and, where noted, a real local kind cluster; its exact
stdout/stderr is captured and rendered into a dark terminal-window screenshot
with Playwright. Nothing here hand-edits command output — long output is
truncated only at explicit, labeled points.

Requires: playwright (pip install playwright && playwright install chromium),
kind, kubectl, and a built ./bin/tenantctl (this script builds it itself).
"""
import os
import re
import subprocess
import sys
import textwrap

from playwright.sync_api import sync_playwright

ROOT = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))
OUT_DIR = os.path.join(ROOT, "docs", "screenshots")
BIN = os.path.join(ROOT, "bin", "tenantctl")
CTX = "kind-tenant-demo-shots"
PROMPT_HOST = "tenalisriharsha@dev k8s-multi-tenant-platform %"

TEMPLATE = """
<html>
<head>
<style>
  body {{ margin: 0; background: #0d1117; }}
  .window {{
    width: 860px;
    background: #1a1b26;
    border-radius: 10px;
    overflow: hidden;
    font-family: "SF Mono", Menlo, Consolas, monospace;
    box-shadow: 0 0 0 1px #2a2b3d;
  }}
  .titlebar {{
    background: #24253a;
    padding: 10px 14px;
    display: flex;
    align-items: center;
    gap: 8px;
  }}
  .dot {{ width: 12px; height: 12px; border-radius: 50%; }}
  .dot.red {{ background: #ff5f56; }}
  .dot.yellow {{ background: #ffbd2e; }}
  .dot.green {{ background: #27c93f; }}
  .title {{
    flex: 1;
    text-align: center;
    color: #9a9cb8;
    font-size: 13px;
    margin-right: 54px;
  }}
  .body {{
    padding: 18px 22px 22px 22px;
    color: #d4d4e0;
    font-size: 14px;
    line-height: 1.5;
    white-space: pre-wrap;
    word-break: break-word;
  }}
  .prompt {{ color: #5fd77f; font-weight: 600; }}
  .cmd {{ color: #e8e8f0; font-weight: 600; }}
</style>
</head>
<body>
<div class="window">
  <div class="titlebar">
    <div class="dot red"></div><div class="dot yellow"></div><div class="dot green"></div>
    <div class="title">{title}</div>
  </div>
  <div class="body">{body}</div>
</div>
</body>
</html>
"""


def esc(s):
    return (
        s.replace("&", "&amp;")
        .replace("<", "&lt;")
        .replace(">", "&gt;")
    )


def truncate(text, head_lines, note):
    """Keep the first head_lines lines verbatim, replace everything after
    with an explicit, labeled truncation note. head_lines must land on an
    object boundary (a "---" document separator) in the real output, so the
    note's object count is accurate rather than cutting an object in half.
    """
    lines = text.splitlines()
    if len(lines) <= head_lines:
        return text
    return "\n".join(lines[:head_lines]) + f"\n... ({note})"


def truncate_head_tail(text, head_lines, tail_lines, note):
    """Like truncate, but also keeps the last tail_lines lines verbatim, so
    a long multi-document stream shows both its start and its final
    document intact with the middle explicitly elided.
    """
    lines = text.splitlines()
    if len(lines) <= head_lines + tail_lines:
        return text
    head = lines[:head_lines]
    tail = lines[-tail_lines:]
    return "\n".join(head) + f"\n... ({note})\n" + "\n".join(tail)


def diff_summary(text):
    """Collapse a tenantctl update --diff output to only its "- " / "+ "
    drift lines, with an explicit count of the unchanged lines omitted.
    Context lines (unprefixed, meaning "present in both") carry no signal
    for a preview screenshot, and the diff's own ordering (every current
    line first, every desired-only line last) means a naive head-N
    truncation would cut off the "+" side entirely.
    """
    lines = text.splitlines()
    changed = [l for l in lines if l.startswith("- ") or l.startswith("+ ")]
    omitted = len(lines) - len(changed)
    return "\n".join(changed) + (
        f"\n... ({omitted} unchanged lines omitted — only the actual drift is shown above)"
    )


def run(cmd, cwd=ROOT, env=None, check=False):
    """Run cmd for real and return (returncode, combined stdout+stderr)."""
    print(f"+ {' '.join(cmd)}")
    full_env = os.environ.copy()
    if env:
        full_env.update(env)
    proc = subprocess.run(
        cmd, cwd=cwd, env=full_env, capture_output=True, text=True
    )
    combined = proc.stdout
    if proc.stderr:
        combined += proc.stderr
    if check and proc.returncode != 0:
        print(combined)
        raise SystemExit(f"command failed: {' '.join(cmd)}")
    return proc.returncode, combined


def shell(cmd_str, cwd=ROOT, check=False):
    print(f"+ {cmd_str}")
    proc = subprocess.run(
        cmd_str, cwd=cwd, shell=True, capture_output=True, text=True
    )
    combined = proc.stdout
    if proc.stderr:
        combined += proc.stderr
    if check and proc.returncode != 0:
        print(combined)
        raise SystemExit(f"command failed: {cmd_str}")
    return proc.returncode, combined


class Shot:
    def __init__(self, filename, title):
        self.filename = filename
        self.title = title
        self.lines = []  # list of (prompt_cmd_or_None, text)

    def command(self, display_cmd, output):
        self.lines.append((display_cmd, output))

    def render_body(self):
        parts = []
        for display_cmd, output in self.lines:
            if display_cmd is not None:
                parts.append(
                    f'<span class="prompt">{esc(PROMPT_HOST)}</span> '
                    f'<span class="cmd">{esc(display_cmd)}</span>'
                )
            if output:
                parts.append(esc(output.rstrip("\n")))
        return "\n".join(parts)


def capture(page, shot: Shot):
    html = TEMPLATE.format(title=esc(shot.title), body=shot.render_body())
    page.set_content(html)
    el = page.locator(".window")
    path = os.path.join(OUT_DIR, shot.filename)
    el.screenshot(path=path)
    print(f"saved {path}")


def main():
    os.makedirs(OUT_DIR, exist_ok=True)

    print("=== building tenantctl ===")
    run(["go", "build", "-o", BIN, "./cmd/tenantctl"], check=True)

    print("=== bringing up kind cluster ===")
    rc, _ = run(["kind", "get", "clusters"])
    shell(f"kind get clusters | grep -qx tenant-demo-shots || kind create cluster --name tenant-demo-shots", check=True)

    shots = []

    # 1. root help
    cmd = "./bin/tenantctl --help"
    _, out = shell(cmd, check=True)
    s = Shot("01-help.png", "tenantctl — command overview")
    s.command(cmd, out)
    shots.append(s)

    # 2. generate (offline)
    cmd = "./bin/tenantctl generate --name acme-corp --admin-name acme-admins"
    _, out = shell(cmd, check=True)
    out = truncate_head_tail(
        out, 26, 15,
        "output truncated — 5 more objects: LimitRange, 3x NetworkPolicy, Role",
    )
    s = Shot("02-generate.png", "tenantctl generate — offline manifest bundle")
    s.command(cmd, out)
    shots.append(s)

    # 3. create + verify on real cluster
    shell(f"kubectl --context {CTX} delete namespace acme-corp --ignore-not-found --wait=true", check=False)
    cmd = f"./bin/tenantctl create --name acme-corp --admin-name acme-admins --context {CTX}"
    _, out = shell(cmd, check=True)
    s = Shot("03-create-and-verify.png", "tenantctl create — onboarding acme-corp on a real kind cluster")
    s.command(cmd, out)
    cmd2 = f"kubectl --context {CTX} get resourcequota,limitrange,networkpolicy,role,rolebinding -n acme-corp"
    _, out2 = shell(cmd2, check=True)
    s.command(cmd2, out2)
    shots.append(s)

    # 4. list with a second tenant
    shell(f"kubectl --context {CTX} delete namespace beta-inc --ignore-not-found --wait=true", check=False)
    shell(f"./bin/tenantctl create --name beta-inc --admin-name beta-admins --context {CTX}", check=True)
    cmd = f"./bin/tenantctl list --context {CTX}"
    _, out = shell(cmd, check=True)
    s = Shot("04-list.png", "tenantctl list — every tenantctl-managed tenant")
    s.command(cmd, out)
    shots.append(s)

    # 5. live network isolation proof
    for ns in ("acme-corp", "beta-inc"):
        shell(
            f"for i in $(seq 1 30); do "
            f"kubectl --context {CTX} get serviceaccount default -n {ns} >/dev/null 2>&1 && break; "
            f"sleep 1; done",
            check=False,
        )
    shell(
        f"kubectl --context {CTX} run probe --image=busybox:1.36 -n acme-corp --restart=Never "
        f"--command -- sleep 3600",
        check=True,
    )
    shell(f"kubectl --context {CTX} wait --for=condition=Ready pod/probe -n acme-corp --timeout=60s", check=True)
    shell(
        f"kubectl --context {CTX} run target --image=busybox:1.36 -n acme-corp --restart=Never "
        f"--command -- httpd -f -p 8080 -h /etc",
        check=True,
    )
    shell(f"kubectl --context {CTX} wait --for=condition=Ready pod/target -n acme-corp --timeout=60s", check=True)
    shell(
        f"kubectl --context {CTX} run other --image=busybox:1.36 -n beta-inc --restart=Never "
        f"--command -- httpd -f -p 8080 -h /etc",
        check=True,
    )
    shell(f"kubectl --context {CTX} wait --for=condition=Ready pod/other -n beta-inc --timeout=60s", check=True)

    target_ip_rc, target_ip = shell(
        f"kubectl --context {CTX} get pod target -n acme-corp -o jsonpath='{{.status.podIP}}'", check=True
    )
    other_ip_rc, other_ip = shell(
        f"kubectl --context {CTX} get pod other -n beta-inc -o jsonpath='{{.status.podIP}}'", check=True
    )

    s = Shot("05-network-isolation.png", "live NetworkPolicy proof — cross-namespace blocked, same-namespace + DNS allowed")

    cmd = f"kubectl --context {CTX} exec -n acme-corp probe -- wget -qO- -T 3 {target_ip.strip()}:8080/hostname"
    _, out = shell(cmd, check=False)
    s.command(cmd, out)

    cmd = f"kubectl --context {CTX} exec -n acme-corp probe -- wget -qO- -T 3 {other_ip.strip()}:8080/hostname"
    _, out = shell(cmd, check=False)
    s.command(cmd, out)

    cmd = f"kubectl --context {CTX} exec -n acme-corp probe -- nslookup kubernetes.default.svc.cluster.local"
    _, out = shell(cmd, check=False)
    s.command(cmd, out)
    shots.append(s)

    shell(f"kubectl --context {CTX} delete pod probe target -n acme-corp --wait=false", check=False)
    shell(f"kubectl --context {CTX} delete pod other -n beta-inc --wait=false", check=False)

    # 6. update: reconcile quota + cross-namespace allow-list
    cmd = (
        f"./bin/tenantctl update --name acme-corp --admin-name acme-admins "
        f"--cpu-request 3 --allow-namespace ingress-nginx --context {CTX}"
    )
    _, out = shell(cmd, check=True)
    s = Shot("06-update-reconcile.png", "tenantctl update — reconciling quota and network allow-list")
    s.command(cmd, out)
    cmd2 = f"kubectl --context {CTX} get networkpolicy -n acme-corp"
    _, out2 = shell(cmd2, check=True)
    s.command(cmd2, out2)
    shots.append(s)

    # 7. diff mode
    cmd = (
        f"./bin/tenantctl update --name acme-corp --admin-name acme-admins "
        f"--cpu-request 5 --allow-namespace ingress-nginx --diff --context {CTX}"
    )
    _, out = shell(cmd, check=True)
    out = diff_summary(out)
    s = Shot("07-diff.png", "tenantctl update --diff — live cluster state vs. the desired spec")
    s.command(cmd, out)
    shots.append(s)

    # 8. dry-run
    cmd = "./bin/tenantctl create --name gamma-llc --admin-name gamma-admins --dry-run"
    _, out = shell(cmd, check=True)
    out = truncate(out, 28, "output truncated — 6 more objects: LimitRange, 3x NetworkPolicy, Role, RoleBinding")
    s = Shot("08-dry-run.png", "tenantctl create --dry-run — preview without touching the cluster")
    s.command(cmd, out)
    shots.append(s)

    # 9. GitOps config-file spec
    cmd = "./bin/tenantctl generate -f examples/tenant.yaml"
    _, out = shell(cmd, check=True)
    out = truncate(out, 27, "output truncated — 7 more objects: LimitRange, 4x NetworkPolicy (including allow-cross-namespace from allowedNamespaces), Role, RoleBinding")
    s = Shot("09-gitops-file.png", "tenantctl generate -f — a tenant spec driven entirely from a GitOps file")
    s.command(cmd, out)
    shots.append(s)

    # 10. delete + safety guard
    cmd = f"./bin/tenantctl delete --name acme-corp --context {CTX}"
    _, out = shell(cmd, check=True)
    s = Shot("10-delete-and-safety.png", "tenantctl delete — offboarding, and the guard against unmanaged namespaces")
    s.command(cmd, out)
    shell(f"kubectl --context {CTX} create namespace someones-namespace", check=True)
    cmd2 = f"./bin/tenantctl delete --name someones-namespace --context {CTX}"
    _, out2 = shell(cmd2, check=False)
    s.command(cmd2, out2)
    shell(f"kubectl --context {CTX} delete namespace someones-namespace --wait=false", check=False)
    shots.append(s)

    # 11. spec validation
    s = Shot("11-validation.png", "spec validation — rejected before touching the cluster")
    cmd = "./bin/tenantctl generate --name Not_Valid --admin-name acme-admins"
    _, out = shell(cmd, check=False)
    s.command(cmd, out)
    cmd2 = "./bin/tenantctl generate --name acme-corp --admin-name acme-admins --cpu-request not-a-quantity"
    _, out2 = shell(cmd2, check=False)
    s.command(cmd2, out2)
    shots.append(s)

    print("=== rendering screenshots ===")
    with sync_playwright() as p:
        browser = p.chromium.launch()
        page = browser.new_page(viewport={"width": 1000, "height": 1200})
        for s in shots:
            capture(page, s)
        browser.close()

    print("=== cleaning up cluster namespaces ===")
    shell(f"kubectl --context {CTX} delete namespace beta-inc --ignore-not-found --wait=false", check=False)

    print("done. Tear the cluster down with: kind delete cluster --name tenant-demo-shots")


if __name__ == "__main__":
    main()
