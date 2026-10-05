/// The server's addresses on this phone, and the links people bring to the app.
library;

import 'dart:io';
import 'dart:typed_data';

import 'models.dart';

/// The addresses this phone uses for its server, and what it learned about them. Kotlin
/// keeps them too (the downloads and the route check run without Flutter), so they are
/// stored through the platform channel.
class ServerConfig {
  const ServerConfig({required this.publicUrl, this.localUrl, this.pins = const [], this.serverId, this.deviceId});

  factory ServerConfig.fromJson(Json j) => ServerConfig(
        publicUrl: Uri.parse(j['public_url'] as String? ?? ''),
        localUrl: j['local_url'] is String && (j['local_url'] as String).isNotEmpty ? Uri.parse(j['local_url'] as String) : null,
        pins: [for (final p in (j['pins'] as List? ?? const [])) if (p is String) p],
        serverId: j['server_id'] as String?,
        deviceId: j['device_id'] as String?,
      );

  final Uri publicUrl;
  final Uri? localUrl;

  /// SHA-256 fingerprints (lowercase hex) of the local address's certificate, when it is https.
  final List<String> pins;

  /// From /api/info; the local address must answer with the same id to be used.
  final String? serverId;

  /// This phone's id on the server, for the proof a server gives over plain http before the
  /// phone sends its key there (HomeProof in Kotlin).
  final String? deviceId;

  /// An address at home to use: plain http, or https with the server's own pinned certificate.
  bool get hasLocal => localUrl != null && (localUrl!.scheme == 'http' || pins.isNotEmpty);

  /// A server only at home: its public address is plain http too.
  bool get publicIsHttp => publicUrl.scheme == 'http';

  /// Takes what the server says about itself, e.g. from a login or an invite.
  ServerConfig withInfo(ServerInfo info) {
    final public = normalizePublicAddress(info.publicUrl) ?? publicUrl;
    final local = info.localUrl == null ? null : normalizeLocalAddress(info.localUrl!);
    return ServerConfig(publicUrl: public, localUrl: local, pins: info.localCertSha256, serverId: serverId, deviceId: deviceId);
  }

  ServerConfig copyWith({Uri? localUrl, bool clearLocal = false, List<String>? pins, String? serverId, String? deviceId}) =>
      ServerConfig(
        publicUrl: publicUrl,
        localUrl: clearLocal ? null : (localUrl ?? this.localUrl),
        pins: clearLocal ? const [] : (pins ?? this.pins),
        serverId: serverId ?? this.serverId,
        deviceId: deviceId ?? this.deviceId,
      );

  Json toJson() => {
        'public_url': publicUrl.toString(),
        'local_url': localUrl?.toString() ?? '',
        'pins': pins,
        'server_id': serverId,
        'device_id': deviceId,
      };
}

final _scheme = RegExp(r'^[a-zA-Z][a-zA-Z0-9+.-]*://');

/// A host name, an IPv4 address, or an IPv6 address (Uri.host drops its brackets).
final _host = RegExp(r'^([a-z0-9]([a-z0-9-]*[a-z0-9])?(\.[a-z0-9]([a-z0-9-]*[a-z0-9])?)*|[0-9a-f:.]*:[0-9a-f:.]*)$');

/// Turns what someone typed ("share.example.com", "https://share.example.com/",
/// "192.168.1.20:8080") into the server's origin, or null if it can't be one. https, or
/// plain http for an address at home (isHomeHost): without a scheme, an address at home
/// means http, anything else https.
Uri? normalizePublicAddress(String input) => _origin(input);

/// The address at home: plain http, or https with the server's own certificate, which the
/// app pins.
Uri? normalizeLocalAddress(String input) => _origin(input);

Uri? _origin(String input) {
  var text = input.trim();
  if (text.isEmpty) return null;
  if (!_scheme.hasMatch(text)) {
    final bare = Uri.tryParse('https://$text');
    text = bare != null && isHomeHost(bare.host) ? 'http://$text' : 'https://$text';
  }
  final uri = Uri.tryParse(text);
  if (uri == null || !_host.hasMatch(uri.host.toLowerCase())) return null;
  final scheme = uri.scheme.toLowerCase();
  if (scheme != 'https' && !(scheme == 'http' && isHomeHost(uri.host))) return null;
  return Uri(scheme: scheme, host: uri.host.toLowerCase(), port: uri.hasPort ? uri.port : null);
}

/// Whether a host is on a home network or this phone: the only places where the app speaks
/// plain http, as the server does (contract/home_hosts.json). A name that public DNS could
/// answer doesn't count; it could lead to the internet.
bool isHomeHost(String host) {
  var h = host.toLowerCase();
  if (h.endsWith('.')) h = h.substring(0, h.length - 1);
  if (h.startsWith('[') && h.endsWith(']')) h = h.substring(1, h.length - 1);
  final ip = InternetAddress.tryParse(h);
  if (ip != null) return _homeAddress(ip);
  if (h == 'localhost' || h.endsWith('.localhost')) return true;
  for (final suffix in const ['.local', '.home.arpa', '.internal']) {
    if (h.endsWith(suffix) && h.length > suffix.length) return true;
  }
  return false;
}

bool _homeAddress(InternetAddress a) {
  if (a.isLoopback || a.isLinkLocal) return true;
  final b = a.rawAddress;
  if (a.type == InternetAddressType.IPv4) {
    return b[0] == 10 || (b[0] == 172 && b[1] >= 16 && b[1] <= 31) || (b[0] == 192 && b[1] == 168);
  }
  if ((b[0] & 0xfe) == 0xfc) return true; // unique local, fc00::/7
  final mapped = b.sublist(0, 10).every((x) => x == 0) && b[10] == 0xff && b[11] == 0xff;
  return mapped && _homeAddress(InternetAddress.fromRawAddress(Uint8List.fromList(b.sublist(12))));
}

/// Something a person scanned, pasted or opened.
sealed class ShareLink {
  const ShareLink();
}

/// An invite: `<server>/join#shi_…`, or `com.eschgi.share://join?server=…&token=…`, the form
/// the invite page hands to the app.
class InviteLink extends ShareLink {
  const InviteLink(this.server, this.token, {this.secret});
  final Uri server;
  final String token;

  /// The secret after the token, which opens the keys the inviting device locked with it
  /// (docs/e2ee-plan.md); it never goes to the server.
  final String? secret;
}

/// A PIN link, `<server>/#K7M2Q`: the app sends with that PIN, as the website does.
class PinLink extends ShareLink {
  const PinLink(this.server, this.code, {this.secret});
  final Uri server;
  final String code;

  /// For a PIN that shows an encrypted folder, after a dot: the secret that opens it.
  final String? secret;
}

final _inviteToken = RegExp(r'^shi_[A-Za-z0-9_-]{20,}$');
final _pinCode = RegExp(r'^[2-9A-HJ-NP-Za-hj-np-z]{5}$');

/// A link's secret: 32 bytes in base64url.
final _secret = RegExp(r'^[A-Za-z0-9_-]{43}$');

String? _secretOf(String? s) => s != null && _secret.hasMatch(s) ? s : null;

ShareLink? parseLink(String text) {
  final uri = Uri.tryParse(text.trim());
  if (uri == null) return null;
  if (uri.scheme == 'com.eschgi.share' && uri.host == 'join') {
    final server = normalizePublicAddress(uri.queryParameters['server'] ?? '');
    final token = uri.queryParameters['token'] ?? '';
    return server != null && _inviteToken.hasMatch(token) ? InviteLink(server, token, secret: _secretOf(uri.queryParameters['key'])) : null;
  }
  if (uri.scheme != 'https' && !(uri.scheme == 'http' && isHomeHost(uri.host))) return null;
  final server = normalizePublicAddress('${uri.scheme}://${uri.authority}');
  if (server == null) return null;
  final fragment = Uri.decodeComponent(uri.fragment);
  final dot = fragment.indexOf('.');
  final head = dot < 0 ? fragment : fragment.substring(0, dot);
  final secret = dot < 0 ? null : _secretOf(fragment.substring(dot + 1));
  if (uri.path == '/join' && _inviteToken.hasMatch(head)) return InviteLink(server, head, secret: secret);
  if ((uri.path == '/' || uri.path.isEmpty) && _pinCode.hasMatch(head)) return PinLink(server, head.toUpperCase(), secret: secret);
  return null;
}
