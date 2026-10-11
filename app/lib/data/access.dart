/// Cloudflare Access in front of the server (docs/cloudflare-access-plan.md): the service token
/// the app shows Cloudflare, since it can't sign in to Cloudflare as a browser does.
library;

import 'dart:async';
import 'dart:convert';

import 'api.dart';
import 'models.dart';
import 'platform.dart';
import 'session.dart' show NotShareServer;

/// A service token: a Client ID and a Client Secret, sent as headers with every request to the
/// public address of the server at [host]. It belongs to the phone, not to the account, and
/// never goes to the local address, which skips Cloudflare, or to a bucket.
class CloudflareAccess {
  const CloudflareAccess({required this.host, required this.clientId, required this.clientSecret, this.on = true});

  factory CloudflareAccess.fromJson(Json j) => CloudflareAccess(
        host: j['host'] as String? ?? '',
        clientId: j['client_id'] as String? ?? '',
        clientSecret: j['client_secret'] as String? ?? '',
        on: j['on'] as bool? ?? true,
      );

  /// From an invite's link, `<client id>:<client secret>` (linkValue); null if it isn't one.
  static CloudflareAccess? fromLink(String host, String? value) {
    if (value == null) return null;
    final colon = value.indexOf(':');
    if (colon < 0) return null;
    final id = value.substring(0, colon), secret = value.substring(colon + 1);
    return validId(id) && validSecret(secret) ? CloudflareAccess(host: host.toLowerCase(), clientId: id, clientSecret: secret) : null;
  }

  /// The public address's host, lowercase.
  final String host;
  final String clientId;
  final String clientSecret;

  /// Switched off in the settings, without forgetting it.
  final bool on;

  /// Whether it goes with a request to [base]: the public address, over https.
  bool appliesTo(Uri base) => on && base.scheme == 'https' && base.host.toLowerCase() == host;

  Map<String, String> get headers => {'CF-Access-Client-Id': clientId, 'CF-Access-Client-Secret': clientSecret};

  /// How it goes into an invite's QR code, after `&cfa=`.
  String get linkValue => '$clientId:$clientSecret';

  CloudflareAccess copyWith({bool? on}) => CloudflareAccess(host: host, clientId: clientId, clientSecret: clientSecret, on: on ?? this.on);

  Json toJson() => {'host': host, 'client_id': clientId, 'client_secret': clientSecret, 'on': on};

  /// Cloudflare's IDs end in `.access`; the part before is hex.
  static bool validId(String id) => _id.hasMatch(id);

  static bool validSecret(String secret) => _secretPattern.hasMatch(secret);
}

final _id = RegExp(r'^[A-Za-z0-9-]+\.access$');
final _secretPattern = RegExp(r'^[A-Za-z0-9]{16,}$');

final _idHeader = RegExp(r'''cf-access-client-id["']?\s*[:=]\s*["']?([A-Za-z0-9-]+\.access)''', caseSensitive: false);
final _secretHeader = RegExp(r'''cf-access-client-secret["']?\s*[:=]\s*["']?([A-Za-z0-9]{16,})''', caseSensitive: false);
final _anyId = RegExp(r'(?<![A-Za-z0-9-])([A-Za-z0-9-]+\.access)(?![A-Za-z0-9.])');
final _anySecret = RegExp(r'(?<![A-Za-z0-9.])([A-Fa-f0-9]{64})(?![A-Za-z0-9])');

/// What Paste from Cloudflare (screen 112) finds in the clipboard: Cloudflare copies both headers
/// together, `CF-Access-Client-Id: …` and `CF-Access-Client-Secret: …`, which may also come as
/// JSON or in a curl command; or just one of the two values.
({String? id, String? secret}) accessFromClipboard(String text) {
  final id = _idHeader.firstMatch(text)?.group(1) ?? _anyId.firstMatch(text)?.group(1);
  final secret = _secretHeader.firstMatch(text)?.group(1) ?? _anySecret.firstMatch(text)?.group(1);
  return (id: id, secret: secret);
}

/// Why Cloudflare Access keeps the app out of this phone's server, for the library's banner
/// (screen 116).
enum AccessProblem {
  /// There is no token for the server, or it is switched off.
  missing,

  /// Cloudflare turned the token away: expired, or revoked.
  refused,
}

/// The token on this phone, kept with the other secrets. Kotlin reads it from there too, for
/// the transfers (CloudflareAccess.kt).
class AccessRepository {
  AccessRepository({required this.api, required this.platform});

  final Api api;
  final Platform platform;
  final _changes = StreamController<CloudflareAccess?>.broadcast();

  static const key = 'cloudflare_access';

  CloudflareAccess? get current => api.access;

  /// The token changed: added, switched on or off, or forgotten.
  Stream<CloudflareAccess?> get changes => _changes.stream;

  /// The token for [server], if there is one, on or off.
  CloudflareAccess? forServer(Uri server) {
    final a = api.access;
    return a != null && a.host == server.host.toLowerCase() ? a : null;
  }

  Future<void> restore() async {
    final raw = await platform.readSecret(key);
    CloudflareAccess? a;
    if (raw != null) {
      try {
        a = CloudflareAccess.fromJson(jsonDecode(raw) as Json);
      } on FormatException {
        a = null;
      }
    }
    api.access = a != null && a.host.isNotEmpty && CloudflareAccess.validId(a.clientId) ? a : null;
  }

  Future<void> save(CloudflareAccess? a) async {
    await platform.writeSecret(key, a == null ? null : jsonEncode(a.toJson()));
    api.access = a;
    _changes.add(a);
  }

  /// Asks the server's public address with [a] whether Cloudflare lets it through to Share.
  /// Throws CloudflareAccessException if Cloudflare turns it away; NotShareServer if Cloudflare
  /// lets it through to something that isn't Share, such as another app's page; ApiException
  /// when nothing answers behind Cloudflare (its 502 or 530); NetworkException without any answer.
  Future<void> check(Uri server, CloudflareAccess a) async {
    final Json info;
    try {
      info = await api.getFrom(server, '/api/info', access: a);
    } on FormatException {
      throw const NotShareServer(); // a page, not Share's JSON
    } on CloudflareAccessException {
      rethrow;
    } on ApiException catch (e) {
      if (e.fromShare || e.status >= 500) rethrow;
      throw const NotShareServer();
    }
    if (info['server_id'] is! String) throw const NotShareServer();
  }
}
