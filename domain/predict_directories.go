package domain

// ChangeSetDirectories maps each directory path to the ids of the change sets in it. Forward never sends a "/" key: every key is at
// least one level deep, an empty network is {}, and the change sets in no directory are ListChangeSets minus every id in the map.
type ChangeSetDirectories map[string][]Identifier

// ChangeSetTagAction is how PatchChangeSetsTags changes the tags of the named change sets.
type ChangeSetTagAction string

const (
	ChangeSetTagsAdd       ChangeSetTagAction = "ADD"
	ChangeSetTagsRemove    ChangeSetTagAction = "REMOVE"
	ChangeSetTagsRemoveAll ChangeSetTagAction = "REMOVE_ALL"
)
