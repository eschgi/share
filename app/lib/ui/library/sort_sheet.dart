import 'package:flutter/material.dart';

import '../../data/models.dart';
import '../../l10n/app_localizations.dart';
import '../icons.dart';
import '../theme.dart';
import '../widgets.dart';

/// Screen 120: how the library is sorted, and newest or oldest first. Each tap changes the
/// library behind at once; the sheet stays for the other choice.
Future<void> showSortSheet(BuildContext context, {required LibrarySort sort, required bool oldest, required void Function(LibrarySort, bool) onChanged}) =>
    showModalBottomSheet<void>(
      context: context,
      isScrollControlled: true,
      builder: (_) => _SortSheet(sort: sort, oldest: oldest, onChanged: onChanged),
    );

/// The line under the filters while the library isn't sorted the usual way (screen 121).
String? sortLine(AppLocalizations t, LibrarySort sort, bool oldest) => switch ((sort, oldest)) {
      (LibrarySort.received, false) => null,
      (LibrarySort.received, true) => t.sortLineReceivedOldest,
      (LibrarySort.taken, false) => t.sortLineTakenNewest,
      (LibrarySort.taken, true) => t.sortLineTakenOldest,
      (LibrarySort.sender, false) => t.sortLineSenderNewest,
      (LibrarySort.sender, true) => t.sortLineSenderOldest,
    };

class _SortSheet extends StatefulWidget {
  const _SortSheet({required this.sort, required this.oldest, required this.onChanged});
  final LibrarySort sort;
  final bool oldest;
  final void Function(LibrarySort, bool) onChanged;

  @override
  State<_SortSheet> createState() => _SortSheetState();
}

class _SortSheetState extends State<_SortSheet> {
  late LibrarySort _sort = widget.sort;
  late bool _oldest = widget.oldest;

  void _set(LibrarySort sort, bool oldest) {
    if (sort == _sort && oldest == _oldest) return;
    setState(() {
      _sort = sort;
      _oldest = oldest;
    });
    widget.onChanged(sort, oldest);
  }

  @override
  Widget build(BuildContext context) {
    final t = AppLocalizations.of(context);
    final c = context.colors;
    Widget option(LibrarySort s, String title, String help) => Semantics(
          selected: _sort == s,
          child: SettingsRow(
            leading: _Radio(on: _sort == s),
            title: title,
            subtitle: help,
            accent: _sort == s,
            trailing: const SizedBox.shrink(),
            onTap: () => _set(s, _oldest),
          ),
        );
    return SafeArea(
      child: SingleChildScrollView(
        padding: const EdgeInsets.fromLTRB(16, 0, 16, 16),
        child: Column(crossAxisAlignment: CrossAxisAlignment.stretch, children: [
          Padding(
            padding: const EdgeInsets.fromLTRB(8, 4, 8, 14),
            child: Text(t.sortTitle, style: Theme.of(context).textTheme.headlineSmall),
          ),
          SettingsGroup(children: [
            option(LibrarySort.taken, t.sortTaken, t.sortTakenHelp),
            option(LibrarySort.received, t.sortReceived, t.sortReceivedHelp),
            option(LibrarySort.sender, t.sortSender, t.sortSenderHelp),
          ]),
          Padding(padding: const EdgeInsets.fromLTRB(8, 22, 8, 8), child: Text(t.sortOrder, style: const TextStyle(fontSize: 15, fontWeight: FontWeight.w500))),
          _Order(oldest: _oldest, onChanged: (o) => _set(_sort, o)),
          Padding(
            padding: const EdgeInsets.fromLTRB(8, 12, 8, 0),
            child: Text(t.sortRemembered, style: TextStyle(fontSize: 13, height: 1.5, color: c.text3)),
          ),
        ]),
      ),
    );
  }
}

class _Radio extends StatelessWidget {
  const _Radio({required this.on});
  final bool on;

  @override
  Widget build(BuildContext context) {
    final c = context.colors;
    return Container(
      width: 40,
      height: 40,
      alignment: Alignment.center,
      child: Container(
        width: 20,
        height: 20,
        alignment: Alignment.center,
        decoration: BoxDecoration(shape: BoxShape.circle, border: Border.all(width: 2, color: on ? c.accentText : c.text3)),
        child: on ? Container(width: 9, height: 9, decoration: BoxDecoration(shape: BoxShape.circle, color: c.accentText)) : null,
      ),
    );
  }
}

/// Newest first, or oldest first.
class _Order extends StatelessWidget {
  const _Order({required this.oldest, required this.onChanged});
  final bool oldest;
  final ValueChanged<bool> onChanged;

  @override
  Widget build(BuildContext context) {
    final t = AppLocalizations.of(context);
    final c = context.colors;
    Widget option(bool value, String label) {
      final selected = oldest == value;
      return Expanded(
        child: Semantics(
          selected: selected,
          button: true,
          child: InkWell(
            borderRadius: BorderRadius.circular(14),
            onTap: () => onChanged(value),
            child: Container(
              height: 50,
              alignment: Alignment.center,
              decoration: BoxDecoration(color: selected ? c.accentSoft : null, borderRadius: BorderRadius.circular(14)),
              child: Row(mainAxisSize: MainAxisSize.min, children: [
                if (selected) ...[Icon(AppIcons.check, size: 20, color: c.accentText), const SizedBox(width: 8)],
                Flexible(
                  child: Text(label,
                      overflow: TextOverflow.ellipsis,
                      style: TextStyle(fontSize: 16, fontWeight: FontWeight.w600, color: selected ? c.accentText : c.text2)),
                ),
              ]),
            ),
          ),
        ),
      );
    }

    return Container(
      padding: const EdgeInsets.all(5),
      decoration: BoxDecoration(color: c.s1, borderRadius: BorderRadius.circular(18), border: Border.all(color: c.line)),
      child: Row(children: [option(false, t.sortNewest), option(true, t.sortOldest)]),
    );
  }
}
