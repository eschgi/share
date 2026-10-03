import 'dart:async';

import 'package:flutter/material.dart';

import '../app.dart';
import '../data/platform.dart';
import '../data/server.dart';
import '../l10n/app_localizations.dart';
import 'icons.dart';
import 'widgets.dart';

/// Screen 16: the two addresses. The app uses the local one whenever it answers and proves to
/// be the same server, otherwise the public one.
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

  @override
  void initState() {
    super.initState();
    final s = Services.read(context);
    final c = s.api.config;
    _public.text = c?.publicUrl.toString() ?? '';
    _local.text = c?.localUrl?.toString() ?? '';
    s.platform.route().then((r) => mounted ? setState(() => _route = r) : null);
    _routes = s.platform.routes.listen((r) => setState(() => _route = r));
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
