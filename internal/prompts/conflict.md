This branch (`{{.Branch}}`) no longer rebases cleanly onto `{{.Base}}`.

Rebase it yourself: run `git rebase --onto {{.Base}} {{.OldBase}}`, resolve every conflict keeping the intent of both sides, run the project's checks, and finish the rebase. If a conflict needs a product decision, stop with the rebase still in progress and explain what the choice is.
