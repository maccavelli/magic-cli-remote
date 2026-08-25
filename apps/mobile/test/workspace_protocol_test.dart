import 'package:flutter_test/flutter_test.dart';

import 'package:magic_cli_remote/data/protocol/models.dart';

/// Workspace wire shapes (MADR 0112 A5, PLAN P6).
///
/// Decoding is deliberately forgiving about missing fields and strict about
/// nothing: the daemon has already validated and bounded everything. What these
/// pin is that a malformed or partial payload degrades to something inert
/// rather than throwing inside a sheet build.
void main() {
  group('WorkspaceEntry', () {
    test('decodes a full row', () {
      final e = WorkspaceEntry.fromJson(const {
        'name': 'main.dart',
        'path': 'lib/main.dart',
        'dir': false,
        'ignored': true,
      });
      expect(e.name, 'main.dart');
      expect(e.path, 'lib/main.dart');
      expect(e.dir, isFalse);
      expect(e.ignored, isTrue);
    });

    test('a directory row is flagged', () {
      final e = WorkspaceEntry.fromJson(const {'path': 'lib', 'dir': true});
      expect(e.dir, isTrue);
      expect(e.ignored, isFalse);
    });

    test('an empty payload is inert rather than throwing', () {
      final e = WorkspaceEntry.fromJson(const {});
      expect(e.name, isEmpty);
      expect(e.path, isEmpty);
      expect(e.dir, isFalse);
    });
  });

  group('WorkspaceContent', () {
    test('decodes text and its byte count', () {
      final c = WorkspaceContent.fromJson(const {
        'path': 'a.txt',
        'text': 'hello',
        'bytes': 5,
      });
      expect(c.path, 'a.txt');
      expect(c.text, 'hello');
      expect(c.bytes, 5);
    });

    test('an empty payload yields empty text, not null', () {
      final c = WorkspaceContent.fromJson(const {});
      expect(c.text, isEmpty);
      expect(c.bytes, 0);
    });
  });

  group('WorkspaceMatch', () {
    test('decodes a positioned hit', () {
      final m = WorkspaceMatch.fromJson(const {
        'path': 'a.go',
        'line': 12,
        'column': 4,
        'text': 'hit',
      });
      expect(m.path, 'a.go');
      expect(m.line, 12);
      expect(m.column, 4);
      expect(m.text, 'hit');
    });

    test('a file-search hit carries only a path', () {
      final m = WorkspaceMatch.fromJson(const {'path': 'a.go'});
      expect(m.line, 0);
      expect(m.column, 0);
      expect(m.text, isEmpty);
    });
  });

  group('WorkspaceSearchResult', () {
    test('decodes matches and the cap that applied', () {
      final r = WorkspaceSearchResult.fromJson(const {
        'kind': 'text',
        'cap': 10,
        'truncated': true,
        'matches': [
          {'path': 'a.go', 'line': 1},
          {'path': 'b.go', 'line': 2},
        ],
      });
      expect(r.kind, 'text');
      expect(r.cap, 10);
      expect(r.truncated, isTrue);
      expect(r.matches, hasLength(2));
      expect(r.matches.first.path, 'a.go');
    });

    test('text and file searches report different caps', () {
      final text = WorkspaceSearchResult.fromJson(const {
        'kind': 'text',
        'cap': 10,
      });
      final file = WorkspaceSearchResult.fromJson(const {
        'kind': 'file',
        'cap': 100,
      });
      expect(
        text.cap,
        isNot(file.cap),
        reason: 'text search is capped upstream at ten; file search is not',
      );
    });

    test('non-map match entries are skipped rather than crashing', () {
      final r = WorkspaceSearchResult.fromJson(const {
        'kind': 'text',
        'matches': ['not a map', 42, null],
      });
      expect(r.matches, isEmpty);
    });

    test('an empty payload is inert', () {
      final r = WorkspaceSearchResult.fromJson(const {});
      expect(r.kind, isEmpty);
      expect(r.matches, isEmpty);
      expect(r.cap, 0);
      expect(r.truncated, isFalse);
    });
  });

  group('SessionCapabilities.workspaceRead', () {
    test('defaults to false so the affordance stays hidden', () {
      final caps = SessionCapabilities.fromJson(const {});
      expect(caps.workspaceRead, isFalse);
    });

    test('is true only when the daemon says so', () {
      final caps = SessionCapabilities.fromJson(const {'workspace_read': true});
      expect(caps.workspaceRead, isTrue);
    });

    test('participates in equality so a change repaints', () {
      final off = SessionCapabilities.fromJson(const {});
      final on = SessionCapabilities.fromJson(const {'workspace_read': true});
      expect(off == on, isFalse);
      expect(off.hashCode == on.hashCode, isFalse);
    });
  });

  group('diagnostics sections', () {
    test('skills decode to name and description only', () {
      final d = SessionDiagnostics.fromJson(const {
        'skills': [
          {
            'name': 'customize-opencode',
            'description': 'Author skills',
            // Fields the daemon strips; defended again on the client.
            'location': '/Users/secret/SKILL.md',
            'content': 'SECRET BODY',
          },
        ],
      });
      expect(d.skills, hasLength(1));
      expect(d.skills.single.name, 'customize-opencode');
      expect(d.skills.single.description, 'Author skills');
    });

    test('language services decode to name and status', () {
      final d = SessionDiagnostics.fromJson(const {
        'lsp': [
          {'name': 'gopls', 'status': 'running', 'root': '/Users/secret'},
        ],
      });
      expect(d.lsp.single.name, 'gopls');
      expect(d.lsp.single.status, 'running');
    });

    test('formatters carry an extension count, never a list', () {
      final d = SessionDiagnostics.fromJson(const {
        'formatters': [
          {'name': 'gofmt', 'enabled': true, 'extensions': 3},
        ],
      });
      expect(d.formatters.single.enabled, isTrue);
      expect(d.formatters.single.extensions, 3);
    });

    test('non-map rows are skipped rather than crashing', () {
      final d = SessionDiagnostics.fromJson(const {
        'skills': ['not a map', 7, null],
        'lsp': 'not a list',
        'formatters': <dynamic>[],
      });
      expect(d.skills, isEmpty);
      expect(d.lsp, isEmpty);
      expect(d.formatters, isEmpty);
    });

    test('an empty payload yields empty sections', () {
      final d = SessionDiagnostics.fromJson(const {});
      expect(d.skills, isEmpty);
      expect(d.lsp, isEmpty);
      expect(d.formatters, isEmpty);
      expect(d.mcp, isEmpty);
    });

    test('malformed rows default rather than throwing', () {
      final d = SessionDiagnostics.fromJson(const {
        'skills': [<String, dynamic>{}],
        'lsp': [<String, dynamic>{}],
        'formatters': [<String, dynamic>{}],
      });
      expect(d.skills.single.name, isEmpty);
      expect(d.lsp.single.status, isEmpty);
      expect(d.formatters.single.extensions, 0);
      expect(d.formatters.single.enabled, isFalse);
    });
  });

  group('SessionCapabilities.skillRefresh', () {
    test('defaults to false so the affordance stays hidden', () {
      expect(SessionCapabilities.fromJson(const {}).skillRefresh, isFalse);
    });

    test('is true only when the daemon says so', () {
      expect(
        SessionCapabilities.fromJson(const {
          'skill_refresh': true,
        }).skillRefresh,
        isTrue,
      );
    });

    test('participates in equality so a change repaints', () {
      final off = SessionCapabilities.fromJson(const {});
      final on = SessionCapabilities.fromJson(const {'skill_refresh': true});
      expect(off == on, isFalse);
      expect(off.hashCode == on.hashCode, isFalse);
    });
  });

  group('SessionCapabilities.shell', () {
    test('defaults to false so the affordance stays hidden', () {
      expect(SessionCapabilities.fromJson(const {}).shell, isFalse);
    });

    test('is true only when the daemon says so', () {
      expect(SessionCapabilities.fromJson(const {'shell': true}).shell, isTrue);
    });

    test('participates in equality so a policy change repaints', () {
      final off = SessionCapabilities.fromJson(const {});
      final on = SessionCapabilities.fromJson(const {'shell': true});
      expect(off == on, isFalse);
      expect(off.hashCode == on.hashCode, isFalse);
    });
  });
}
