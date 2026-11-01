# Penguins glossary

## Penguin

One individual bird measured at Palmer Station, one row in `penguins`. The data has no bird id, so Slaw generated one on import (`slaw import --key auto`). It identifies a row, not a bird.

## Body mass

Weight in grams. Two birds have no recorded mass; the average leaves them out.

## Flipper length

Length of the flipper in millimetres. The same two birds have none.

## Gentoo share

The fraction of birds in a group that are Gentoo. A ratio of counts: it is never summed across groups.

## Sex

`male` or `female`, as judged in the field. Eleven birds are recorded with no sex, which is why the `not_null` check on `sex` fails.
