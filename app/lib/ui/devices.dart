import 'package:clock/clock.dart';
import 'package:flutter/material.dart';

import '../app.dart';
import '../data/api.dart';
import '../data/models.dart';
import '../l10n/app_localizations.dart';
import 'format.dart';
import 'icons.dart';
import 'theme.dart';
import 'widgets.dart';

/// One phone or browser of an account: when it was last used, and Sign out for the others.
class PhoneRow extends StatelessWidget {
  const PhoneRow({super.key, required this.phone, this.onSignOut});
  final Phone phone;

  /// Null while busy; the phone asking has no button.
  final VoidCallback? onSignOut;

  @override
  Widget build(BuildContext context) {
    final t = AppLocalizations.of(context);
    final locale = Localizations.localeOf(context).languageCode;
    return SettingsRow(
      leading: SettingsRow.icon(context, phone.isBrowser ? AppIcons.monitor : AppIcons.smartphone),
      title: phone.name,
      subtitle: [
        phone.isThis ? t.personThisPhone : t.personLastUsed(formatWhen(phone.lastSeenAt, clock.now(), locale)),
        if (phone.homeOnly) t.personAtHome,
      ].join(' · '),
      trailing: phone.isThis
          ? const SizedBox()
          : TextButton(
              style: TextButton.styleFrom(foregroundColor: context.colors.danger),
              onPressed: onSignOut,
              child: Text(t.personSignOutPhone),
            ),
    );
  }
}

/// Asks before signing [phone] out; true to go ahead.
Future<bool> askToSignOut(BuildContext context, Phone phone) async {
  final t = AppLocalizations.of(context);
  return await showDialog<bool>(
        context: context,
        builder: (context) => AlertDialog(
          title: Text(t.personSignOutTitle(phone.name)),
          content: Text(t.personSignOutBody, style: TextStyle(color: context.colors.text2, height: 1.5)),
          actions: [
            TextButton(onPressed: () => Navigator.pop(context, false), child: Text(t.commonCancel)),
            TextButton(
              style: TextButton.styleFrom(foregroundColor: context.colors.danger),
              onPressed: () => Navigator.pop(context, true),
              child: Text(t.personSignOutPhone),
            ),
          ],
        ),
      ) ??
      false;
}

/// One's own phones and browsers, from the profile card in Settings: a lost one, or a browser
/// left signed in on someone else's computer, can be signed out without an admin.
class MyDevicesSheet extends StatefulWidget {
  const MyDevicesSheet({super.key});

  @override
  State<MyDevicesSheet> createState() => _MyDevicesSheetState();
}

class _MyDevicesSheetState extends State<MyDevicesSheet> {
  List<Phone>? _devices;
  String? _problem;
  bool _busy = false;

  @override
  void initState() {
    super.initState();
    _load();
  }

  Future<void> _load() async {
    final session = Services.read(context).session;
    try {
      final devices = await session.myDevices();
      if (mounted) setState(() => _devices = devices);
    } on ApiException {
      if (mounted) setState(() => _problem = AppLocalizations.of(context).commonFailed);
    } on NetworkException {
      if (mounted) setState(() => _problem = AppLocalizations.of(context).commonOffline);
    }
  }

  Future<void> _signOut(Phone phone) async {
    if (!await askToSignOut(context, phone) || !mounted) return;
    final t = AppLocalizations.of(context);
    final messenger = ScaffoldMessenger.of(context);
    setState(() => _busy = true);
    try {
      await Services.read(context).session.signOutDevice(phone.id);
      if (mounted) setState(() => _devices = [for (final d in _devices ?? const <Phone>[]) if (d.id != phone.id) d]);
    } on ApiException {
      messenger.showSnackBar(SnackBar(content: Text(t.commonFailed)));
    } on NetworkException {
      messenger.showSnackBar(SnackBar(content: Text(t.commonOffline)));
    } finally {
      if (mounted) setState(() => _busy = false);
    }
  }

  @override
  Widget build(BuildContext context) {
    final t = AppLocalizations.of(context);
    final c = context.colors;
    final devices = _devices;
    return SafeArea(
      child: SingleChildScrollView(
        padding: const EdgeInsets.fromLTRB(20, 4, 20, 12),
        child: Column(crossAxisAlignment: CrossAxisAlignment.stretch, mainAxisSize: MainAxisSize.min, children: [
          Text(t.myDevicesTitle, style: Theme.of(context).textTheme.headlineSmall),
          const SizedBox(height: 8),
          Text(t.myDevicesLead, style: TextStyle(fontSize: 15, color: c.text2, height: 1.5)),
          const SizedBox(height: 16),
          if (_problem != null)
            Text(_problem!, style: TextStyle(fontSize: 15, color: c.danger))
          else if (devices == null)
            const Center(child: Padding(padding: EdgeInsets.all(16), child: CircularProgressIndicator()))
          else
            SettingsGroup(children: [
              for (final d in devices) PhoneRow(phone: d, onSignOut: _busy ? null : () => _signOut(d)),
            ]),
        ]),
      ),
    );
  }
}
