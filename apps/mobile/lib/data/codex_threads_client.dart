import 'protocol/models.dart';

abstract mixin class CodexThreadsClient {
  Future<CodexThreadsSnapshot> load({bool archived = false});
  Future<CodexThreadsPage> search(String term);
  Future<void> archive(String threadId, bool archived);
  Future<void> rename(String threadId, String name);
  Future<void> fork(String threadId);
  Future<CodexDeletePreview> previewDelete(String threadId);
  Future<void> delete(String threadId);
  Future<void> moveThread(
    String threadId,
    String? sectionId, {
    String? beforeThreadId,
  });
  Future<void> deleteSection(String sectionId);

  Future<CodexThreadsPage> loadMore(
    String cursor, {
    bool archived = false,
  }) async => CodexThreadsPage();

  Future<void> createSection(
    String name, {
    String icon = '',
    String color = '',
  }) async => throw UnsupportedError('thread section creation unavailable');

  Future<void> updateSection(
    String sectionId,
    String name, {
    String icon = '',
    String color = '',
  }) async => throw UnsupportedError('thread section update unavailable');

  Future<void> createProject(String name, List<String> roots) async =>
      throw UnsupportedError('project creation unavailable');

  Future<void> importProject(
    String name,
    List<String> roots,
    List<String> threadIds,
  ) async => throw UnsupportedError('project import unavailable');

  Future<void> updateProject(
    String projectId,
    String name,
    List<String> roots,
  ) async => throw UnsupportedError('project update unavailable');

  Future<void> moveProject(String projectId, String? beforeProjectId) async =>
      throw UnsupportedError('project move unavailable');

  Future<void> deleteProject(String projectId) async =>
      throw UnsupportedError('project deletion unavailable');

  Future<void> assignProject(String threadId, String? projectId) async =>
      throw UnsupportedError('project assignment unavailable');
}
