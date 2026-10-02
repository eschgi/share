import 'package:flutter/material.dart';

import '../app.dart';
import '../l10n/app_localizations.dart';
import 'icons.dart';
import 'widgets.dart';

/// Where Share's source code is; the repository also has the license texts.
const sourceUrl = 'https://github.com/eschgi/share';

/// The app's version and, signed in, the server's; the license, the source code, and the
/// licenses of what Share is built with. Also from PIN sending, without the server's version,
/// which only people with an account see.
class AboutScreen extends StatefulWidget {
  const AboutScreen({super.key, required this.signedIn});
  final bool signedIn;

  @override
  State<AboutScreen> createState() => _AboutScreenState();
}

class _AboutScreenState extends State<AboutScreen> {
  String? _app, _server;

  @override
  void initState() {
    super.initState();
    final services = Services.read(context);
    services.platform.appVersion().then((v) {
      if (mounted) setState(() => _app = v.code > 0 ? '${v.name} (${v.code})' : v.name);
    });
    if (widget.signedIn) {
      services.api.get('/api/about').then((r) {
        if (mounted) setState(() => _server = r['version'] as String? ?? '');
      }, onError: (Object _) {});
    }
  }

  @override
  Widget build(BuildContext context) {
    final t = AppLocalizations.of(context);
    final platform = Services.read(context).platform;
    return Scaffold(
      appBar: AppBar(leading: const ShareBackButton(), title: Text(t.aboutTitle)),
      body: ListView(padding: const EdgeInsets.fromLTRB(16, 4, 16, 24), children: [
        SettingsGroup(children: [
          SettingsRow(leading: SettingsRow.icon(context, AppIcons.smartphone), title: t.aboutApp, subtitle: _app ?? '…', trailing: const SizedBox()),
          if (widget.signedIn)
            SettingsRow(leading: SettingsRow.icon(context, AppIcons.server), title: t.aboutServer, subtitle: _server ?? '…', trailing: const SizedBox()),
        ]),
        const SizedBox(height: 14),
        SettingsGroup(children: [
          SettingsRow(
            leading: SettingsRow.icon(context, AppIcons.fileText),
            title: t.aboutLicense,
            subtitle: 'Apache License 2.0',
            trailing: const Icon(AppIcons.externalLink, size: 18),
            onTap: () => platform.openUrl('$sourceUrl/blob/main/LICENSE'),
          ),
          SettingsRow(
            leading: SettingsRow.icon(context, AppIcons.share),
            title: t.aboutSource,
            subtitle: sourceUrl.replaceFirst('https://', ''),
            trailing: const Icon(AppIcons.externalLink, size: 18),
            onTap: () => platform.openUrl(sourceUrl),
          ),
          SettingsRow(
            leading: SettingsRow.icon(context, AppIcons.info),
            title: t.aboutLicenses,
            subtitle: t.aboutLicensesSub,
            onTap: () => showLicensePage(
              context: context,
              applicationName: 'Share',
              applicationVersion: _app,
              applicationLegalese: 'Copyright 2026 Stefan Eschgfäller',
            ),
          ),
        ]),
      ]),
    );
  }
}
