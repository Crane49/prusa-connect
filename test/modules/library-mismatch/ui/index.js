// The entry the app must never reach. It throws while it evaluates, so an
// import is visible in the refusal the loader records: a module that ran is
// `reason: 'threw'` and a module that was refused is `reason:
// 'library-mismatch'`.
throw new Error('the library-mismatch fixture was imported, and its range says it must not be');
