**Discord:** https://discord.com/users/168438474905616386

# Paradox File Utils
This is just a place for different tools and utilities I'm currently working on for my Quieter Events mod.

## What's Here

### Paradox Modding Tool

A Cross Platform Desktop Application Modding Tool for speeding up the development of modders of Paradox Interactive Games. Currently It has two funcitons with more to come:

#### Compare Tool

Essential just a diff tool that's most useful to check the difference between multiple files in too different directies that are named the same and share relative paths. Essentially when overriding vanill files you want to be able to see the differences between updates, this tool helps. It can show diff of any too files though as well.

#### Merge Tool

This uses the parser to go through two sets of files and merges matching pairs with one base file and a secondary file, base file always takes precedence unless object keys (eventIDs, accoladeIDs, etc.) are added to a list, then objects matching those keys in the secondary file will take precedence, this way you can update files after a game patch but still keep the oebjects you've modified.

<!-- TODO: 
1. Need to Add Object Extractor Page (uses parser to grab and list out all objects from a given file(s), can probably add export to json or csv funcitonality).
2. Add PDX Info Files/Docs Page, maybe have it point to the game install and it will grab all info files and show them in a pretty/formatted way? Otherwise I would need to store them myself in the rpo which might not be a huge deal but requires constant updates.
3. Extend existing functionality for Merge and Compare Tools, mainly allow mixing and matching files, offer multiple file/folder selection modes for scenarieos (files share content but different names, relative paths are different, etc..)
4. More testing of parser to with ck3 and maybe even EU5 or Stellaris?
-->

### Paradox File Parser

A code parser I created using Go-Participle that is able to parse most Paradox `.txt` files and allows more complex manipulation of entities in those files.

More detail in vanilla-synchronizer [README.md](vanilla-synchronizer/README.md#parser-information)

## Other Paradox Projects

### CK3 Quieter Events Mod
This mod is a successor to the **Less Event Spam** mod. It converts several vanilla events that took up the whole screen or required interaction for no reason into smaller toasts or messages. 
Find it [here](https://github.com/idodavis/ck3-quieter-events)
