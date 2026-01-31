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

### Paradox File Parser

A code parser I created using Go-Participle that is able to parse most Paradox `.txt` files and allows more complex manipulation of entities in those files.

More detail in vanilla-synchronizer [README.md](vanilla-synchronizer/README.md#parser-information)

## Other Paradox Projects

### CK3 Quieter Events Mod
This mod is a successor to the **Less Event Spam** mod. It converts several vanilla events that took up the whole screen or required interaction for no reason into smaller toasts or messages. 
Find it [here](https://github.com/idodavis/ck3-quieter-events)
