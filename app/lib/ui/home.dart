import 'package:flutter/material.dart';

import '../data/models.dart';
import '../l10n/app_localizations.dart';
import 'icons.dart';
import 'library/library_screen.dart';
import 'settings_screen.dart';

/// Signed in: the library and the settings. (Sending from the app comes with the next step;
/// until then the website does it.)
class HomeShell extends StatefulWidget {
  const HomeShell({super.key, required this.user});
  final User user;

  @override
  State<HomeShell> createState() => _HomeShellState();
}

class _HomeShellState extends State<HomeShell> {
  int _tab = 0;

  @override
  Widget build(BuildContext context) {
    final t = AppLocalizations.of(context);
    final nav = NavigationBar(
      selectedIndex: _tab,
      onDestinationSelected: (i) => setState(() => _tab = i),
      destinations: [
        NavigationDestination(icon: const Icon(AppIcons.images), label: t.navLibrary),
        NavigationDestination(icon: const Icon(AppIcons.settings), label: t.navSettings),
      ],
    );
    return IndexedStack(index: _tab, children: [
      LibraryScreen(user: widget.user, navigation: nav, onAvatar: () => setState(() => _tab = 1)),
      SettingsScreen(user: widget.user, navigation: nav),
    ]);
  }
}
