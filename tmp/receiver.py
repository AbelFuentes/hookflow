from http.server import BaseHTTPRequestHandler, HTTPServer

class H(BaseHTTPRequestHandler):
    def do_POST(self):
        body = self.rfile.read(int(self.headers["Content-Length"]))
        print("X-Source:", self.headers.get("X-Source"), "| body:", body.decode(), flush=True)
        self.send_response(204)
        self.end_headers()

HTTPServer(("", 9999), H).serve_forever()