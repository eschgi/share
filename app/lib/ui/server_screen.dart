import 'dart:async';

import 'package:flutter/material.dart';

import '../app.dart';
import '../data/api.dart';
import '../data/platform.dart';
import '../data/server.dart';
import '../l10n/app_localizations.dart';
import 'access_screen.dart';
import 'icons.dart';
import 'widgets.dart';

/// What the last look at Cloudflare Access found (screen 114).
enum _AccessCheck { checking, ok, refused, failed }

/// Screen 16: the two addresses. The app uses the local one whenever it answers and proves to
/// be the same server, otherwise the public one. With Cloudflare Access in front of the server,
/// also its service token (114).
class ServerScreen extends StatefulWidget {
  const ServerScreen({super.key});

  @override
  State<ServerScreen> createState() => _ServerScreenState();
}

class _ServerScreenState extends State<ServerScreen> {
  final _public = TextEditingController();
  final _local = TextEditingController();
  RouteStatus? _route;
  StreamSubscription<RouteStatus>? _routes;
  String? _localError;
  bool _checking = false;
  _AccessCheck? _access;

  @override
  void initState() {
    super.initState();
    final s = Services.read(context);
    final c = s.api.config;
    _public.text = c?.publicUrl.toString() ?? '';
    _local.text = c?.localUrl?.toString() ?? '';
    s.platform.route().then((r) => mounted ? setState(() => _route = r) : null);
    _routes = s.platform.routes.listen((r) => setState(() => _route = r));
    WidgetsBinding.instance.addPostFrameCallback((_) => _checkAccess());
  }

  Uri? get _server => Services.read(context).api.config?.publicUrl;

  /// Asks Cloudflare over the public address whether the token gets through.
  Future<void> _checkAccess() async {
    final server = _server;
    final access = server == null ? null : Services.read(context).access.forServer(server);
    if (server == null || access == null || !access.on) return setState(() => _access = null);
    setState(() => _access = _AccessCheck.checking);
    _AccessCheck result;
    try {
      await Services.read(context).access.check(server, access);
      result = _AccessCheck.ok;
    } on CloudflareAccessException {
      result = _AccessCheck.refused;
    } on ApiException catch (e) {
      result = e.fromShare ? _AccessCheck.ok : _AccessCheck.failed;
    } on NetworkException {
      result = _AccessCheck.failed;
    }
    if (mounted) setState(() => _access = result);
  }

  /// Turns the token on, asking for one if there is none yet, or off without forgetting it.
  Future<void> _switchAccess(bool on) async {
    final server = _server;
    if (server == null) return;
    final repo = Services.read(context).access;
    final known = repo.forServer(server);
    if (on && known == null) {
      final saved = await Navigator.push<bool>(context, MaterialPageRoute(builder: (_) => AccessTokenScreen(server: server)));
      if (saved == true && mounted) setState(() => _access = _AccessCheck.ok);
      return;
    }
    if (known == null) return;
    await repo.save(known.copyWith(on: on));
    if (!mounted) return;
    on ? await _checkAccess() : setState(() => _access = null);
  }

  Future<void> _editAccess() async {
    final server = _server;
    if (server == null) return;
    final saved = await Navigator.push<bool>(context, MaterialPageRoute(builder: (_) => AccessTokenScreen(server: server)));
    if (saved == true && mounted) setState(() => _access = _AccessCheck.ok);
  }

  List<Widget> _accessPart(AppLocalizations t) {
    final server = _server;
    final access = server == null ? null : Services.of(context).access.forServer(server);
    final on = access?.on == true;
    final a = _access;
    return [
      const SizedBox(height: 8),
      SectionLabel(t.accessTitle),
      SettingsGroup(children: [
        SettingsRow(
          leading: SettingsRow.icon(context, AppIcons.shield),
          title: t.accessSwitch,
          subtitle: on ? t.accessOnlyPublic : t.accessSwitchHelp,
          trailing: Switch(value: on, onChanged: _switchAccess),
          onTap: () => _switchAccess(!on),
        ),
        if (access != null && on) ...[
          SettingsRow(leading: SettingsRow.icon(context, AppIcons.key), title: t.accessClientId, subtitle: access.clientId, onTap: _editAccess),
          SettingsRow(leading: SettingsRow.icon(context, AppIcons.lock), title: t.accessClientSecret, subtitle: '•' * 16, onTap: _editAccess),
        ],
      ]),
      if (on && a != null) ...[
        const SizedBox(height: 14),
        StatusCard(
          ok: a == _AccessCheck.ok,
          busy: a == _AccessCheck.checking,
          title: switch (a) {
            _AccessCheck.checking => t.routeChecking,
            _AccessCheck.ok => t.accessOk,
            _AccessCheck.refused => t.accessRefused,
            _AccessCheck.failed => t.accessUnchecked,
          },
          detail: switch (a) {
            _AccessCheck.ok || _AccessCheck.checking => t.accessOkDetail,
            _AccessCheck.refused => t.accessRefusedDetail,
            _AccessCheck.failed => t.commonOffline,
          },
        ),
      ],
    ];
  }

  @override
  void dispose() {
    _routes?.cancel();
    super.dispose();
  }

  Future<void> _check() async {
    final t = AppLocalizations.of(context);
    final s = Services.read(context);
    final text = _local.text.trim();
    final local = text.isEmpty ? null : normalizeLocalAddress(text);
    if (text.isNotEmpty && local == null) {
      setState(() => _localError = t.serverLocalBadAddress);
      return;
    }
    setState(() {
      _localError = null;
      _checking = true;
    });
    if (local.toString() != s.api.config?.localUrl.toString()) await s.session.setLocalAddress(local);
    final r = await s.platform.route(check: true);
    if (mounted) {
      setState(() {
        _route = r;
        _checking = false;
      });
      unawaited(_checkAccess());
    }
  }

  @override
  Widget build(BuildContext context) {
    final t = AppLocalizations.of(context);
    final r = _route;
    final local = r?.isLocal == true;
    final detail = local
        ? t.routeUsingLocalDetail(r!.millis ?? 0)
        : switch (r?.reason) {
            RouteReason.noLocal || null => t.routeNoLocal,
            RouteReason.unreachable => t.routeLocalUnreachable,
            RouteReason.wrongCertificate => t.routeLocalWrongCert,
            RouteReason.otherServer => t.routeLocalOtherServer,
            RouteReason.none => t.routeLocalUnreachable,
          };
    return Scaffold(
      appBar: AppBar(
        leading: const ShareBackButton(),
        title: Text(t.serverTitle, style: const TextStyle(fontFamily: 'Roboto', fontSize: 20, fontWeight: FontWeight.w600)),
      ),
      body: SafeArea(
        child: LayoutBuilder(
          builder: (context, box) => SingleChildScrollView(
            padding: const EdgeInsets.fromLTRB(24, 4, 24, 16),
            child: ConstrainedBox(
              constraints: BoxConstraints(minHeight: box.maxHeight - 20),
              child: IntrinsicHeight(
                child: Column(crossAxisAlignment: CrossAxisAlignment.stretch, children: [
          FieldLabel(t.serverPublic, first: true),
          TextField(controller: _public, readOnly: true),
          Help(t.serverPublicHelp),
          FieldLabel(t.serverLocal, optional: t.serverOptional),
          TextField(
            controller: _local,
            keyboardType: TextInputType.url,
            autocorrect: false,
            decoration: InputDecoration(hintText: 'http://192.168.1.20:8080', errorText: _localError, errorMaxLines: 3),
            onSubmitted: (_) => _check(),
          ),
          Help(t.serverLocalHelp),
          ..._accessPart(t),
          const SizedBox(height: 20),
          StatusCard(
            ok: local,
            busy: _checking || r?.checking == true,
            title: _checking ? t.routeChecking : (local ? t.routeUsingLocal : t.routeUsingPublic),
            detail: detail,
          ),
                  const Spacer(),
                  const SizedBox(height: 24),
                  OutlinedButton.icon(
                    onPressed: _checking ? null : _check,
                    icon: const Icon(AppIcons.refresh, size: 20),
                    label: Text(t.serverCheckAgain),
                  ),
                  Small(t.serverInviteFills),
                ]),
              ),
            ),
          ),
        ),
      ),
    );
  }
}
