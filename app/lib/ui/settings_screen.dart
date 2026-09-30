import 'dart:async';

import 'package:flutter/material.dart';

import '../app.dart';
import '../data/api.dart';
import '../data/models.dart';
import '../data/platform.dart';
import '../l10n/app_localizations.dart';
import 'icons.dart';
import 'server_screen.dart';
import 'theme.dart';
import 'widgets.dart';

/// Screen 17, what everyone sees: profile, language, server, password, signing out and
/// deleting the account. (Admins get PINs, people and storage with the next step.)
class SettingsScreen extends StatefulWidget {
  const SettingsScreen({super.key, required this.user, required this.navigation});
  final User user;
  final Widget navigation;

  @override
  State<SettingsScreen> createState() => _SettingsScreenState();
}

class _SettingsScreenState extends State<SettingsScreen> {
  RouteStatus? _route;
  StreamSubscription<RouteStatus>? _routes;

  @override
  void initState() {
    super.initState();
    final p = Services.read(context).platform;
    p.route().then((r) => mounted ? setState(() => _route = r) : null);
    _routes = p.routes.listen((r) => setState(() => _route = r));
  }

  @override
  void dispose() {
    _routes?.cancel();
    super.dispose();
  }

  String _languageName(AppLocalizations t, String code) => switch (code) {
        'de' => t.languageGerman,
        'it' => t.languageItalian,
        _ => t.languageEnglish,
      };

  Future<void> _pickLanguage() async {
    final t = AppLocalizations.of(context);
    final services = Services.read(context);
    const none = '';
    final choice = await showDialog<String>(
      context: context,
      builder: (context) => SimpleDialog(title: Text(t.settingsLanguage), children: [
        for (final (code, label) in [
          (none, t.settingsLanguageSystem),
          ('en', t.languageEnglish),
          ('de', t.languageGerman),
          ('it', t.languageItalian),
        ])
          SimpleDialogOption(
            onPressed: () => Navigator.pop(context, code),
            child: Padding(padding: const EdgeInsets.symmetric(vertical: 6), child: Text(label, style: const TextStyle(fontSize: 16))),
          ),
      ]),
    );
    if (choice != null) await services.setLanguage(choice == none ? null : choice);
  }

  Future<void> _password() async {
    await showDialog<void>(context: context, builder: (_) => _PasswordDialog(user: widget.user));
  }

  Future<bool> _confirm(String title, String body, String action, {bool danger = false}) async {
    final t = AppLocalizations.of(context);
    final c = context.colors;
    return await showDialog<bool>(
          context: context,
          builder: (context) => AlertDialog(
            title: Text(title),
            content: Text(body, style: TextStyle(color: c.text2, height: 1.5)),
            actions: [
              TextButton(onPressed: () => Navigator.pop(context, false), child: Text(t.commonCancel)),
              TextButton(
                style: TextButton.styleFrom(foregroundColor: danger ? c.danger : c.accentText),
                onPressed: () => Navigator.pop(context, true),
                child: Text(action),
              ),
            ],
          ),
        ) ??
        false;
  }

  Future<void> _signOut() async {
    final t = AppLocalizations.of(context);
    if (await _confirm(t.settingsSignOut, t.settingsSignOutConfirm, t.settingsSignOut) && mounted) {
      await Services.read(context).session.signOut();
    }
  }

  Future<void> _delete() async {
    final t = AppLocalizations.of(context);
    final messenger = ScaffoldMessenger.of(context);
    if (!await _confirm(t.settingsDeleteTitle, t.settingsDeleteBody, t.settingsDeleteButton, danger: true) || !mounted) return;
    try {
      await Services.read(context).session.deleteAccount();
    } on ApiException catch (e) {
      messenger.showSnackBar(SnackBar(content: Text(e.code == 'last_admin' ? t.settingsLastAdmin : t.commonFailed)));
    } on NetworkException {
      messenger.showSnackBar(SnackBar(content: Text(t.commonOffline)));
    }
  }

  @override
  Widget build(BuildContext context) {
    final t = AppLocalizations.of(context);
    final c = context.colors;
    final u = widget.user;
    final chosen = Services.of(context).language.value;
    final phone = Localizations.localeOf(context).languageCode;
    return Scaffold(
      appBar: AppBar(titleSpacing: 22, title: Text(t.settingsTitle)),
      bottomNavigationBar: widget.navigation,
      body: ListView(padding: const EdgeInsets.fromLTRB(16, 4, 16, 24), children: [
        Container(
          padding: const EdgeInsets.all(14),
          decoration: BoxDecoration(color: c.s1, borderRadius: BorderRadius.circular(20), border: Border.all(color: c.lineSoft)),
          child: Row(children: [
            Avatar(name: u.name, id: u.id, size: 52),
            const SizedBox(width: 14),
            Expanded(
              child: Column(crossAxisAlignment: CrossAxisAlignment.start, children: [
                Row(children: [
                  Flexible(child: Text(u.name, overflow: TextOverflow.ellipsis, style: const TextStyle(fontSize: 18, fontWeight: FontWeight.w600))),
                  const SizedBox(width: 8),
                  RoleBadge(label: u.isAdmin ? t.roleAdmin : t.roleMember, admin: u.isAdmin),
                ]),
                const SizedBox(height: 2),
                Text(t.settingsSignedInHere, style: TextStyle(fontSize: 13, color: c.text3)),
              ]),
            ),
          ]),
        ),
        const SizedBox(height: 14),
        SettingsGroup(children: [
          SettingsRow(
            leading: SettingsRow.icon(context, AppIcons.globe),
            title: t.settingsLanguage,
            subtitle: chosen == null ? t.settingsLanguageAutomatic(_languageName(t, phone)) : _languageName(t, chosen),
            onTap: _pickLanguage,
          ),
          SettingsRow(
            leading: SettingsRow.icon(context, AppIcons.server),
            title: t.settingsServer,
            subtitle: _route?.isLocal == true ? t.settingsServerLocal : t.settingsServerPublic,
            onTap: () => Navigator.push(context, MaterialPageRoute<void>(builder: (_) => const ServerScreen())),
          ),
        ]),
        const SizedBox(height: 14),
        SettingsGroup(children: [
          SettingsRow(
            leading: SettingsRow.icon(context, AppIcons.key),
            title: t.settingsPassword,
            subtitle: u.hasPassword && u.username != null ? t.settingsPasswordSet(u.username!) : t.settingsPasswordNone,
            onTap: _password,
          ),
          SettingsRow(leading: SettingsRow.icon(context, AppIcons.logOut), title: t.settingsSignOut, onTap: _signOut, trailing: const SizedBox()),
          SettingsRow(
            leading: SettingsRow.icon(context, AppIcons.userX),
            title: t.settingsDeleteAccount,
            danger: true,
            onTap: _delete,
            trailing: const SizedBox(),
          ),
        ]),
      ]),
    );
  }
}

class _PasswordDialog extends StatefulWidget {
  const _PasswordDialog({required this.user});
  final User user;

  @override
  State<_PasswordDialog> createState() => _PasswordDialogState();
}

class _PasswordDialogState extends State<_PasswordDialog> {
  late final _username = TextEditingController(text: widget.user.username ?? widget.user.name.toLowerCase().replaceAll(' ', '.'));
  final _current = TextEditingController();
  final _password = TextEditingController();
  String? _usernameError, _currentError, _passwordError;
  bool _busy = false;

  Future<void> _save() async {
    final t = AppLocalizations.of(context);
    setState(() {
      _usernameError = _currentError = null;
      _passwordError = _password.text.length < 8 ? t.settingsPasswordTooShort : null;
    });
    if (_passwordError != null) return;
    setState(() => _busy = true);
    final navigator = Navigator.of(context);
    try {
      await Services.read(context).session.setPassword(username: _username.text.trim(), password: _password.text, current: _current.text);
      navigator.pop();
    } on ApiException catch (e) {
      setState(() {
        switch (e.code) {
          case 'password_wrong':
            _currentError = t.settingsPasswordWrong;
          case 'username_taken':
            _usernameError = t.settingsUsernameTaken;
          case 'bad_request':
            _usernameError = t.settingsUsernameBad;
          default:
            _passwordError = t.commonFailed;
        }
      });
    } on NetworkException {
      setState(() => _passwordError = t.commonOffline);
    } finally {
      if (mounted) setState(() => _busy = false);
    }
  }

  @override
  Widget build(BuildContext context) {
    final t = AppLocalizations.of(context);
    return AlertDialog(
      title: Text(t.settingsPassword),
      content: SingleChildScrollView(
        child: Column(mainAxisSize: MainAxisSize.min, crossAxisAlignment: CrossAxisAlignment.stretch, children: [
          FieldLabel(t.signInUsername, first: true),
          TextField(controller: _username, autocorrect: false, decoration: InputDecoration(errorText: _usernameError)),
          if (widget.user.hasPassword) ...[
            FieldLabel(t.settingsPasswordCurrent),
            TextField(controller: _current, obscureText: true, decoration: InputDecoration(errorText: _currentError)),
          ],
          FieldLabel(t.settingsPasswordNew),
          TextField(controller: _password, obscureText: true, decoration: InputDecoration(errorText: _passwordError)),
        ]),
      ),
      actions: [
        TextButton(onPressed: () => Navigator.pop(context), child: Text(t.commonCancel)),
        TextButton(onPressed: _busy ? null : _save, child: Text(t.commonSave)),
      ],
    );
  }
}
