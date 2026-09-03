// 临时本地 HTTPS CONNECT 代理（来自Trae）。
// 用途：企业网络下 node 的 c-ares 直接 DNS 查询被拒（ECONNREFUSED/EAI_FAIL），
// 而 Windows 的 Resolve-DnsName 走 DNS 客户端缓存可正常解析。
// 本代理收到 npm 的 CONNECT 请求后，用 Resolve-DnsName 解析目标域名，
// 再与目标 IP 建立隧道，从而让 npm 下载不受 node DNS 故障影响。
// 用完即删，不修改任何系统配置。
const http = require("http");
const net = require("net");
const { execFile } = require("child_process");

const PORT = 8765;
const cache = new Map();

function resolveViaWindows(host) {
  return new Promise((resolve, reject) => {
    if (cache.has(host)) return resolve(cache.get(host));
    execFile(
      "powershell",
      ["-NoProfile", "-NonInteractive", "-Command",
        `(Resolve-DnsName ${host} -Type A -ErrorAction SilentlyContinue | Where-Object {$_.IPAddress} | Select-Object -First 1 -ExpandProperty IPAddress)`],
      { timeout: 15000, windowsHide: true, encoding: "utf8" },
      (err, stdout) => {
        if (err) return reject(err);
        const ip = String(stdout || "").trim();
        if (!ip) return reject(new Error("no A record for " + host));
        cache.set(host, ip);
        resolve(ip);
      },
    );
  });
}

const server = http.createServer((req, res) => {
  res.writeHead(502, { "Content-Type": "text/plain" });
  res.end("only CONNECT is supported");
});

server.on("connect", (req, clientSocket, head) => {
  const [host, portStr] = req.url.split(":");
  const port = Number(portStr) || 443;
  resolveViaWindows(host)
    .then((ip) => {
      const upstream = net.connect(port, ip);
      upstream.on("connect", () => {
        clientSocket.write("HTTP/1.1 200 Connection Established\r\n\r\n");
        if (head && head.length) upstream.write(head);
        clientSocket.pipe(upstream);
        upstream.pipe(clientSocket);
      });
      upstream.on("error", () => {
        try { clientSocket.destroy(); } catch (_) {}
      });
      clientSocket.on("error", () => {
        try { upstream.destroy(); } catch (_) {}
      });
    })
    .catch(() => {
      try { clientSocket.destroy(); } catch (_) {}
    });
});

server.listen(PORT, "127.0.0.1", () => {
  console.log(`[npm-proxy] ready on 127.0.0.1:${PORT}`);
});
