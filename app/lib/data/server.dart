/// The server's addresses on this phone, and the links people bring to the app.
library;

import 'models.dart';

/// The addresses this phone uses for its server, and what it learned about them. Kotlin
/// keeps them too (the downloads and the route check run without Flutter), so they are
/// stored through the platform channel.
class ServerConfig {
  const ServerConfig({required this.publicUrl, this.localUrl, this.pins = const [], this.serverId});

  factory ServerConfig.fromJson(Json j) => ServerConfig(
        publicUrl: Uri.parse(j['public_url'] as String? ?? ''),
        localUrl: j['local_url'] is String && (j['local_url'] as String).isNotEmpty ? Uri.parse(j['local_url'] as String) : null,
        pins: [for (final p in (j['pins'] as List? ?? const [])) if (p is String) p],
        serverId: j['server_id'] as String?,
      );

  final Uri publicUrl;
  final Uri? localUrl;

  /// SHA-256 fingerprints (lowercase hex) of the local address's certificate.
  final List<String> pins;

  /// From /api/info; the local address must answer with the same id to be used.
  final String? serverId;

  bool get hasLocal => localUrl != null && pins.isNotEmpty;

  /// Takes what the server says about itself, e.g. from a login or an invite.
  ServerConfig withInfo(ServerInfo info) {
    final public = normalizePublicAddress(info.publicUrl) ?? publicUrl;
    final local = info.localUrl == null ? null : normalizeLocalAddress(info.localUrl!);
    return ServerConfig(publicUrl: public, localUrl: local, pins: info.localCertSha256, serverId: serverId);
  }

  ServerConfig copyWith({Uri? localUrl, bool clearLocal = false, List<String>? pins, String? serverId}) => ServerConfig(
        publicUrl: publicUrl,
        localUrl: clearLocal ? null : (localUrl ?? this.localUrl),
        pins: clearLocal ? const [] : (pins ?? this.pins),
        serverId: serverId ?? this.serverId,
      );

  Json toJson() => {
        'public_url': publicUrl.toString(),
        'local_url': localUrl?.toString() ?? '',
        'pins': pins,
        'server_id': serverId,
      };
}

final _scheme = RegExp(r'^[a-zA-Z][a-zA-Z0-9+.-]*://');

/// A host name, an IPv4 address, or an IPv6 address (Uri.host drops its brackets).
final _host = RegExp(r'^([a-z0-9]([a-z0-9-]*[a-z0-9])?(\.[a-z0-9]([a-z0-9-]*[a-z0-9])?)*|[0-9a-f:.]*:[0-9a-f:.]*)$');

/// Turns what someone typed ("share.example.com", "https://share.example.com/") into the
/// server's origin, or null if it can't be one. Only https: the public address goes over
/// the internet.
Uri? normalizePublicAddress(String input) => _origin(input, allowedSchemes: const {'https'});

/// The local address must be https too; its certificate is self-signed and pinned.
Uri? normalizeLocalAddress(String input) => _origin(input, allowedSchemes: const {'https'});

Uri? _origin(String input, {required Set<String> allowedSchemes}) {
  var text = input.trim();
  if (text.isEmpty) return null;
  if (!_scheme.hasMatch(text)) text = 'https://$text';
  final uri = Uri.tryParse(text);
  if (uri == null || !allowedSchemes.contains(uri.scheme.toLowerCase()) || !_host.hasMatch(uri.host.toLowerCase())) return null;
  return Uri(scheme: uri.scheme.toLowerCase(), host: uri.host.toLowerCase(), port: uri.hasPort ? uri.port : null);
}

/// Something a person scanned, pasted or opened.
sealed class ShareLink {
  const ShareLink();
}

/// An invite: `<server>/join#shi_…`, or `com.eschgi.share://join?server=…&token=…`, the form
/// the invite page hands to the app.
class InviteLink extends ShareLink {
  const InviteLink(this.server, this.token);
  final Uri server;
  final String token;
}

/// A PIN link, `<server>/#K7M2Q`: it opens sending on the website.
class PinLink extends ShareLink {
  const PinLink(this.server, this.code);
  final Uri server;
  final String code;
}

final _inviteToken = RegExp(r'^shi_[A-Za-z0-9_-]{20,}$');
final _pinCode = RegExp(r'^[2-9A-HJ-NP-Za-hj-np-z]{5}$');

ShareLink? parseLink(String text) {
  final uri = Uri.tryParse(text.trim());
  if (uri == null) return null;
  if (uri.scheme == 'com.eschgi.share' && uri.host == 'join') {
    final server = normalizePublicAddress(uri.queryParameters['server'] ?? '');
    final token = uri.queryParameters['token'] ?? '';
    return server != null && _inviteToken.hasMatch(token) ? InviteLink(server, token) : null;
  }
  if (uri.scheme != 'https') return null;
  final server = normalizePublicAddress('${uri.scheme}://${uri.authority}');
  if (server == null) return null;
  final fragment = Uri.decodeComponent(uri.fragment);
  if (uri.path == '/join' && _inviteToken.hasMatch(fragment)) return InviteLink(server, fragment);
  if ((uri.path == '/' || uri.path.isEmpty) && _pinCode.hasMatch(fragment)) return PinLink(server, fragment.toUpperCase());
  return null;
}
