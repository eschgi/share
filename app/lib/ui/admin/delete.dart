import 'package:flutter/material.dart';

import '../../l10n/app_localizations.dart';
import '../icons.dart';
import '../theme.dart';
import '../widgets.dart';

/// Screen 21: asks before deleting; deleted files wait [days] days in Recently deleted.
Future<bool> confirmDelete(BuildContext context, int count, int days) async {
  final t = AppLocalizations.of(context);
  final c = context.colors;
  return await showDialog<bool>(
        context: context,
        builder: (context) => Dialog(
          insetPadding: const EdgeInsets.symmetric(horizontal: 22, vertical: 24),
          child: Padding(
            padding: const EdgeInsets.fromLTRB(26, 26, 22, 18),
            child: Column(mainAxisSize: MainAxisSize.min, children: [
              Container(
                width: 60,
                height: 60,
                decoration: BoxDecoration(color: c.dangerSoft, shape: BoxShape.circle),
                child: Icon(AppIcons.trash, size: 28, color: c.danger),
              ),
              const SizedBox(height: 18),
              Text(t.deleteTitle(count), textAlign: TextAlign.center, style: Theme.of(context).textTheme.headlineMedium),
              const SizedBox(height: 12),
              Markup(t.deleteBody(days), textAlign: TextAlign.center, style: TextStyle(fontSize: 16, height: 1.55, color: c.text2)),
              const SizedBox(height: 22),
              Row(mainAxisAlignment: MainAxisAlignment.end, children: [
                TextButton(onPressed: () => Navigator.pop(context, false), child: Text(t.commonCancel)),
                const SizedBox(width: 8),
                FilledButton(
                  style: FilledButton.styleFrom(backgroundColor: c.dangerStrong, foregroundColor: Colors.white, minimumSize: const Size(0, 50)),
                  onPressed: () => Navigator.pop(context, true),
                  child: Text(t.deleteButton(count)),
                ),
              ]),
            ]),
          ),
        ),
      ) ??
      false;
}
