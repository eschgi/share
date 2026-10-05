/// The server's JSON API, over the local address when the route says so and over the
/// public one otherwise. A read that fails over the local address is tried once more over
/// the public one; writes are never repeated.
library;

import 'dart:async';
import 'dart:convert';
import 'dart:io';
import 'dart:typed_data';

import 'package:crypto/crypto.dart';
import 'package:http/http.dart' as http;
import 'package:http/io_client.dart';

import 'models.dart';
import 'platform.dart';
import 'server.dart';

/// An error answer from the server, with its code from contract/errors.json.
class ApiException implements Exception {
  const ApiException(this.status, this.code, this.message, {this.retryAfter, this.attemptsLeft});

  final int status;
  final String code;
  final String message;
  final Duration? retryAfter;
  final int? attemptsLeft;

  /// The phone's key doesn't work any more: it was signed out, or its account deleted.
  bool get signedOut => code == 'signed_out' || (status == 401 && code == 'unauthorized');

  @override
  String toString() => 'ApiException($status $code: $message)';
}

/// The server couldn't be reached at all.
class NetworkException implements Exception {
  const NetworkException(this.cause);
  final Object cause;

  @override
  String toString() => 'NetworkException($cause)';
}

/// Makes the client that talks to the local address. Its certificate is self-signed, so
/// instead of the usual checks the connection is trusted only if the certificate's SHA-256
/// is one of the pins learned over the public address.
typedef LocalClientFactory = http.Client Function(ServerConfig config);

http.Client pinnedClient(ServerConfig config) {
  final local = config.localUrl!;
  final pins = config.pins.toSet();
  final client = HttpClient(context: SecurityContext(withTrustedRoots: false))
    ..connectionTimeout = const Duration(seconds: 3)
    ..badCertificateCallback = (cert, host, port) =>
        host == local.host && port == local.port && pins.contains(sha256.convert(cert.der).toString());
  return IOClient(client);
}

class Api {
  Api({required this.platform, http.Client? publicClient, LocalClientFactory? localClient, this.timeout = const Duration(seconds: 20)})
      : _public = publicClient ?? http.Client(),
        _makeLocal = localClient ?? pinnedClient;

  final Platform platform;
  final Duration timeout;
  final http.Client _public;
  final LocalClientFactory _makeLocal;
  http.Client? _local;
  ServerConfig? _config;

  /// The signed-in phone's key, sent as a bearer token.
  String? token;

  /// Called when the server says this phone's key stopped working.
  void Function()? onSignedOut;

  ServerConfig? get config => _config;

  final _storage = <String, Storage>{};

  /// Where a server keeps its files, asked once: this phone's server over its route, or a
  /// PIN's [server]. A failed answer isn't kept.
  Future<Storage> storage({Uri? server}) async {
    final base = server ?? _config?.publicUrl;
    if (base == null) throw const NetworkException('no server');
    final known = _storage[base.origin];
    if (known != null) return known;
    final info = server == null ? await get('/api/info') : await getFrom(server, '/api/info');
    return _storage[base.origin] = Storage.parse(info['storage']);
  }

  set config(ServerConfig? c) {
    _local?.close();
    _local = null;
    _config = c;
  }

  Future<Json> get(String path, {Map<String, String>? query}) async => _json(await _send('GET', path, query: query));

  Future<Json> post(String path, [Object? body]) async => _json(await _send('POST', path, body: body));

  Future<void> put(String path, Object body) => _send('PUT', path, body: body);

  Future<Json> patch(String path, Object body) async => _json(await _send('PATCH', path, body: body));

  Future<Json> delete(String path) async => _json(await _send('DELETE', path));

  Future<Uint8List> bytes(String path) async => (await _send('GET', path, raw: true)).bodyBytes;

  /// A request to a server this phone isn't set up for yet, e.g. to look at an invite; with
  /// [bearer], as a PIN session.
  Future<Json> getFrom(Uri server, String path, {Map<String, String>? query, String? bearer}) async =>
      _json(await _request(_public, server, 'GET', path, query: query, auth: false, bearer: bearer));

  /// The bytes of a file, such as a thumbnail, as a PIN session sees them.
  Future<Uint8List> bytesFrom(Uri server, String path, {String? bearer}) async =>
      (await _request(_public, server, 'GET', path, auth: false, bearer: bearer)).bodyBytes;

  /// A request to a server this phone isn't signed in to, e.g. to look at an invite; with
  /// [bearer], as a PIN session.
  Future<Json> postTo(Uri server, String path, Object body, {String? bearer}) async =>
      _json(await _request(_public, server, 'POST', path, body: body, auth: false, bearer: bearer));

  Future<http.Response> _send(String method, String path, {Map<String, String>? query, Object? body, bool raw = false}) async {
    final c = _config;
    if (c == null) throw const NetworkException('no server');
    var route = c.hasLocal || c.publicIsHttp ? await platform.route() : RouteStatus.public;
    // Right after signing in, at the start or on a new network the route isn't known yet: wait
    // for the check rather than take the public address, which may not answer at home. The same
    // for a server only at home on a network it wasn't checked on.
    if (route.checking || (c.publicIsHttp && !route.isLocal && !route.publicVerified)) route = await platform.route(check: true);
    if (route.isLocal) {
      try {
        return await _request(_local ??= _makeLocal(c), c.localUrl!, method, path, query: query, body: body);
      } on NetworkException {
        unawaited(platform.route(check: true)); // the local address didn't answer: look again
        if (method != 'GET') rethrow; // it may have arrived; don't do it twice
      }
    }
    // Over plain http the phone's key goes only to a server that proved here to be its own.
    if (c.publicIsHttp && token != null && !route.publicVerified) throw const NetworkException('not this phone\'s server here');
    return _request(_public, c.publicUrl, method, path, query: query, body: body);
  }

  Future<http.Response> _request(http.Client client, Uri base, String method, String path,
      {Map<String, String>? query, Object? body, bool auth = true, String? bearer}) async {
    final uri = base.replace(path: path, queryParameters: query == null || query.isEmpty ? null : query);
    final req = http.Request(method, uri);
    final key = bearer ?? (auth ? token : null);
    if (key != null) req.headers['Authorization'] = 'Bearer $key';
    if (body != null) {
      req.headers['Content-Type'] = 'application/json';
      req.body = jsonEncode(body);
    }
    http.Response res;
    try {
      res = await http.Response.fromStream(await client.send(req).timeout(timeout)).timeout(timeout);
    } on TimeoutException catch (e) {
      throw NetworkException(e);
    } on SocketException catch (e) {
      throw NetworkException(e);
    } on HandshakeException catch (e) {
      throw NetworkException(e); // the local address with another certificate
    } on http.ClientException catch (e) {
      throw NetworkException(e);
    }
    if (res.statusCode >= 200 && res.statusCode < 300) return res;
    final error = _errorOf(res);
    if (error.signedOut && auth && token != null) onSignedOut?.call();
    throw error;
  }

  static ApiException _errorOf(http.Response res) {
    Json? e;
    try {
      final body = jsonDecode(utf8.decode(res.bodyBytes));
      if (body is Map && body['error'] is Map) e = (body['error'] as Map).cast<String, dynamic>();
    } catch (_) {
      // Not ours: e.g. Cloudflare's HTML page when the server is down.
    }
    if (e == null) return ApiException(res.statusCode, res.statusCode >= 500 ? 'unavailable' : 'unknown', res.reasonPhrase ?? '');
    final retry = e['retry_after_seconds'];
    return ApiException(
      res.statusCode,
      e['code'] as String? ?? 'unknown',
      e['message'] as String? ?? '',
      retryAfter: retry is num ? Duration(seconds: retry.toInt()) : null,
      attemptsLeft: (e['attempts_left'] as num?)?.toInt(),
    );
  }

  static Json _json(http.Response res) {
    if (res.statusCode == 204 || res.bodyBytes.isEmpty) return const {};
    final v = jsonDecode(utf8.decode(res.bodyBytes));
    return v is Map ? v.cast<String, dynamic>() : const {};
  }
}
