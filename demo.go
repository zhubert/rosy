package main

// demoFixture is a fabricated rosy-style git-log-p stream used by the hidden
// --demo flag to bring up the TUI without touching gh or claude. It is not
// meant to satisfy diff parity against any real PR — it just exercises enough
// TUI surface area (multiple commits, multiple files per commit, bodies,
// deletions, and a long line) to iterate on the view code.
const demoFixture = `commit 1a2b3c4d5e6f7a8b9c0d1e2f3a4b5c6d7e8f9a0b
Author: Demo Author <demo@example.com>
Date:   Tue Apr 21 10:00:00 2026 +0000

    Cache widget lookups in WidgetStore

    Get was hitting the backing database on every call, even for the same
    id within a single request. Add a per-instance cache keyed by id so
    repeated lookups in the same handler are free after the first one.

diff --git a/pkg/widget/store.go b/pkg/widget/store.go
index 1111111..2222222 100644
--- a/pkg/widget/store.go
+++ b/pkg/widget/store.go
@@ -10,8 +10,15 @@ package widget

 type Store struct {
 	db *sql.DB
+	cache map[string]*Widget
+}
+
+func NewStore(db *sql.DB) *Store {
+	return &Store{db: db, cache: map[string]*Widget{}}
 }

 func (s *Store) Get(id string) (*Widget, error) {
+	if w, ok := s.cache[id]; ok {
+		return w, nil
+	}
 	row := s.db.QueryRow("select id, name, archived from widgets where id = ?", id)
 	var w Widget
 	if err := row.Scan(&w.ID, &w.Name, &w.Archived); err != nil {
@@ -19,5 +26,6 @@ func (s *Store) Get(id string) (*Widget, error) {
 		}
 		return nil, err
 	}
+	s.cache[id] = &w
 	return &w, nil
 }

commit 2b3c4d5e6f7a8b9c0d1e2f3a4b5c6d7e8f9a0b1c
Author: Demo Author <demo@example.com>
Date:   Tue Apr 21 10:06:00 2026 +0000

    Filter archived widgets at the query layer

    Callers started double-filtering on the returned slice, which meant
    every List() call was iterating twice. Push the filter into the query
    so callers can trust the result.

diff --git a/pkg/widget/store.go b/pkg/widget/store.go
index 2222222..3333333 100644
--- a/pkg/widget/store.go
+++ b/pkg/widget/store.go
@@ -34,7 +34,7 @@ func (s *Store) Get(id string) (*Widget, error) {
 }

 func (s *Store) List() ([]*Widget, error) {
-	rows, err := s.db.Query("select id, name, archived from widgets")
+	rows, err := s.db.Query("select id, name, archived from widgets where archived = false")
 	if err != nil {
 		return nil, err
 	}
diff --git a/pkg/widget/store_test.go b/pkg/widget/store_test.go
index 4444444..5555555 100644
--- a/pkg/widget/store_test.go
+++ b/pkg/widget/store_test.go
@@ -20,4 +20,12 @@ func TestListReturnsActiveWidgets(t *testing.T) {
 	if len(got) != 2 {
 		t.Fatalf("want 2 widgets, got %d", len(got))
 	}
 }
+
+func TestListOmitsArchivedWidgets(t *testing.T) {
+	store := newTestStore(t, widget{id: "w1"}, widget{id: "w2", archived: true})
+	got, _ := store.List()
+	if len(got) != 1 || got[0].ID != "w1" {
+		t.Fatalf("expected only w1, got %+v", got)
+	}
+}

commit 3c4d5e6f7a8b9c0d1e2f3a4b5c6d7e8f9a0b1c2d
Author: Demo Author <demo@example.com>
Date:   Tue Apr 21 10:12:00 2026 +0000

    Drop obsolete legacy-archived fixtures

    Now that List filters archived rows at the query layer, the
    legacy-archived fixtures that existed only to exercise the
    client-side filter are dead weight.

diff --git a/testdata/widgets/legacy.yaml b/testdata/widgets/legacy.yaml
deleted file mode 100644
index 6666666..0000000
--- a/testdata/widgets/legacy.yaml
+++ /dev/null
@@ -1,12 +0,0 @@
----
-widgets:
-  - id: w_legacy_01_this_is_an_intentionally_long_identifier_used_to_exercise_soft_wrapping_in_the_diff_pane
-    name: "Ancient Widget"
-    archived: true
-  - id: w_legacy_02
-    name: "Also Ancient"
-    archived: true
-  - id: w_legacy_03
-    name: "Still Ancient"
-    archived: true
-# retained only to assert client-side filter behavior
diff --git a/testdata/fixtures/widgets.yaml b/testdata/fixtures/widgets.yaml
index 7777777..8888888 100644
--- a/testdata/fixtures/widgets.yaml
+++ b/testdata/fixtures/widgets.yaml
@@ -10,9 +10,3 @@ active_one:
   id: w1
   name: "First"
   archived: false
-
-legacy_archived:
-  id: w_legacy_01
-  name: "Ancient Widget"
-  archived: true
-  notes: "kept to drive client-side filter — delete when query filters"

commit 4d5e6f7a8b9c0d1e2f3a4b5c6d7e8f9a0b1c2d3e
Author: Demo Author <demo@example.com>
Date:   Tue Apr 21 10:18:00 2026 +0000

    Rename WidgetStore.Get to Find

diff --git a/pkg/widget/store.go b/pkg/widget/store.go
index 3333333..4444444 100644
--- a/pkg/widget/store.go
+++ b/pkg/widget/store.go
@@ -14,7 +14,7 @@ func NewStore(db *sql.DB) *Store {
 	return &Store{db: db, cache: map[string]*Widget{}}
 }

-func (s *Store) Get(id string) (*Widget, error) {
+func (s *Store) Find(id string) (*Widget, error) {
 	if w, ok := s.cache[id]; ok {
 		return w, nil
 	}
`
