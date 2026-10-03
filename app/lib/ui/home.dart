import 'dart:async';

import 'package:flutter/material.dart';

import '../app.dart';
import '../data/folders.dart';
import '../data/models.dart';
import '../l10n/app_localizations.dart';
import 'icons.dart';
import '../data/platform.dart';
import 'library/library_screen.dart';
import 'send/send_screen.dart';
import 'send/shared.dart';
import 'settings_screen.dart';

/// Signed in: the library, sending, and the settings.
class HomeShell extends StatefulWidget {
  const HomeShell({super.key, required this.user});
  final User user;

  @override
  State<HomeShell> createState() => _HomeShellState();
}

class _HomeShellState extends State<HomeShell> {
  int _tab = 0;
  late final FolderStore _folders = Services.read(context).folders;

  @override
  void initState() {
    super.initState();
    unawaited(_folders.load());
  }

  @override
  void dispose() {
    unawaited(_folders.clear()); // signed out: the next person starts with all folders
    super.dispose();
  }

  @override
  Widget build(BuildContext context) {
    final t = AppLocalizations.of(context);
    final nav = NavigationBar(
      selectedIndex: _tab,
      onDestinationSelected: (i) => setState(() => _tab = i),
      destinations: [
        NavigationDestination(icon: const Icon(AppIcons.images), label: t.navLibrary),
        NavigationDestination(icon: const Icon(AppIcons.upload), label: t.navSend),
        NavigationDestination(icon: const Icon(AppIcons.settings), label: t.navSettings),
      ],
    );
    void toSettings() => setState(() => _tab = 2);
    final tabs = [
      LibraryScreen(user: widget.user, navigation: nav, onAvatar: toSettings),
      SendScreen(user: widget.user, navigation: nav, onAvatar: toSettings),
      SettingsScreen(user: widget.user, navigation: nav),
    ];
    // Files shared from another app go out at once, on the Send tab.
    return SharedSender(
      auth: SendAuth.device,
      onShared: () {
        Navigator.of(context).popUntil((r) => r.isFirst);
        setState(() => _tab = 1);
      },
      // Every tab's Scaffold shows the app's snack bar, each in a Hero of the same tag; only the
      // tab in view may take part when a screen opens or closes on top.
      child: IndexedStack(index: _tab, children: [
        for (final (i, tab) in tabs.indexed) HeroMode(enabled: i == _tab, child: tab),
      ]),
    );
  }
}
