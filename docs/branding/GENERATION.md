# Patio artwork generation notes

The [revision 3 notes](REVISION-3.md) document the current side-view paper-plane grip. The [revision 2 notes](REVISION-2.md) retain the earlier edit history and restoration of the approved warm, bordered app icon.

The artwork was generated with the built-in image-generation tool. The earlier approved cartoon artwork supplied the character, palette, composition, and smooth lowercase wordmark references. The final direction uses wax crayons for the illustrations while retaining the original smooth plum `patio` wordmark. No real photograph of a child was used.

The application icon restores the earlier rounded, textured yellowish-brown tile with its orange-brown border. The exterior is transparent. Production PNG sizes are resampled exports. ICO and ICNS are native format exports using the same encoder library versions as Wails v2.16.0; they are not separate generated drawings.

Initial generation prompts are retained below. The revision notes record subsequent edits; revision 3 is the current paper-plane treatment.

## Logo — crayon character, original smooth wordmark

```text
Use case: style-transfer.
Asset type: final transparent Patio logo.
Input image 1 is the ORIGINAL APPROVED logo and the edit target. Input image 2 is a WAX-CRAYON STYLE REFERENCE for the child only.
Critical user correction: the word "patio" must NOT look crayon-painted. The original smooth lowercase wordmark in image 1 is approved and must stay unchanged: same letter shapes, same solid very dark plum color, same weight, same spacing, same untextured smooth edge, same size and position. This is the most important constraint.
Edit only the illustrated boy, his clothing, shoes, and paper airplane in image 1 into the real wax-crayon medium of image 2. Preserve his exact identity, hair silhouette with one curl, face/expression, pose, body proportions, outfit, plane, and relationship to the p. Keep the hand resting on the p and the entire logo composition the same. His cheeks, hair, yellow shirt, terracotta overalls, raspberry shoes, and plane should show granular wax pigment, directional broad crayon strokes and slightly wobbly dark contours, with the affectionate handmade feel of a children's picture book. No glossy gradients on the boy.
Lettering invariants: the five lowercase letters p-a-t-i-o must remain smooth SOLID DEEP PLUM as in image 1, completely free of grain, paper fibers, speckles, cross-hatching, texture or crayon strokes. Do not recolor the wordmark toward raspberry. The p is a letter, not part of the crayon illustration; preserve its smooth fill even directly beneath his hand.
Output one wide transparent PNG with the whole curl, plane, shoes, wordmark and comfortable clear margins visible. Preserve the original 2:1-ish horizontal framing. Do not crop to touching edges. True alpha outside the illustration and letter counters. No backdrop, checkerboard, new objects, other text or watermark.
```

## App icon — restored warm bordered tile

For this revision, the earlier generated rounded app icon was restored exactly, as requested. Its warm yellowish-brown paper texture, orange-brown border, crayon portrait, and transparent exterior are preserved. No new drawing was generated for the app icon in this revision. Native icon files are regenerated from that restored source.

## README banner

```text
Use case: style-transfer.
Asset type: wide Patio GitHub README banner.
Input image 1 is the exact edit target for layout, text and courtyard scene. Input image 2 is the approved new brand style: the child is drawn in wax crayons, while the lowercase patio wordmark is smooth deep plum.
Redraw the illustration of image 1 in the rich wax-crayon medium of image 2. Preserve the boy's identity, single hair curl, joyful expression and paper-airplane launching pose, yellow shirt, terracotta short overalls, raspberry shoes, sunny courtyard arch, plants and path, and the chat/check/calendar symbols. Use broad hand-colored strokes, granular wax pigment, gently uneven crayon outlines, children's-picture-book warmth. Retain the same layout with clear text on the left and the boy/scenery on the right. Warm ivory paper background with fine subtle paper grain.
MOST IMPORTANT: preserve the top-left "patio" wordmark as the original SMOOTH, UNTEXTURED, solid deep-plum logo. Same rounded letter shapes, proportions, lowercase spelling, and location. No crayon grain in that wordmark.
Keep all copy exact and readable. Primary headline, on three lines:
"Your agents."
"Your work."
"One place."
Supporting description:
"A desktop workspace for AI chats,"
"tasks, and automation."
The headline may have very light hand-colored character but must remain bold and easy to read. Supporting copy must remain crisp and legible. No "Meet Martin", no names, no extra copy or watermarks. Preserve margins and the original very wide 2.5:1 banner shape. One finished image, not a mockup.
```

## Social graphic

```text
Use case: style-transfer.
Asset type: square Patio social announcement graphic.
Input image 1 is the edit target, preserving composition and exact copy. Input image 2 is the approved new crayon style for the character, with an intentionally smooth patio wordmark.
Redraw the child and courtyard artwork of image 1 using the tactile real wax-crayon treatment of image 2. Preserve the seated smiling little boy, one distinctive curled hair tuft, yellow T-shirt, terracotta overalls, raspberry sneakers, paper airplane in his hand, courtyard steps and open arch, foliage and chat/check/calendar doodles. Broad directional coloring strokes, rich but softly broken wax pigment, slightly uneven dark outlines, affectionate hand-drawn children's-picture-book warmth. Use cream paper grain in the background.
Critical invariant: keep the top-left "patio" wordmark exactly like the original in image 1: smooth UNTEXTURED solid very dark plum, rounded lowercase letters, same letter shapes and proportion. Never render the wordmark in crayon.
Keep the headline exact and in the same place:
"Welcome"
"to Patio."
Keep footer exact and readable:
"A desktop workspace for AI chats,"
"tasks, and automation."
"github.com/epulla/patio"
The headline may have a gentle crayon feel, but keep supporting copy clear. Do not add "Meet Martin", another persona name, additional text, labels or watermarks. Preserve original square framing, comfortable margins and text hierarchy. One finished square social image, no mockup.
```

