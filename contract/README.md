# Contract

What the server, the website and the app must agree on, as JSON that each side's tests read.

- `pin_codes.json`: how typed PINs are cleaned up and which inputs are possible PINs at all.
  Read by the server's auth tests and the website's PIN tests.
- `errors.json`: every error code with its HTTP status. The server's end-to-end tests fail on
  a code that isn't listed here.
- `web_routes.json`: the paths the server answers with the website's single page, which routes
  them itself. Read by the server's and the website's tests.
- `app/platform.json`: what the app's Dart and Kotlin halves hand each other over the platform
  channel: the stored server addresses, the files of a download, and the route and transfer
  events. Read by the Flutter tests and the Android unit tests.
- `app/share_zip.json`: the `share.json` in every ZIP the app makes (docs/zip-plan.md): an example,
  and broken or mismatched variants of it that must open as ordinary ZIPs. Read by the Android unit
  tests.
- `api/*.json`: one example request and response per endpoint: `method`, `path`, `auth`
  (`none`, `pin`, `device`, `admin`; `device` and `admin` mean the app's bearer token or a
  signed-in browser's cookie), `request`, `status` and `response`. The server's end-to-end
  tests check that real responses have the same shape (the same fields, nested the same way).
  Values such as ids and times may differ; error codes may not.

When an endpoint changes, change its fixture in the same commit.
