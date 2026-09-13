package model

import "gorm.io/gorm"

// BeforeUpdate advances the credential identity generation whenever the stored
// secret is explicitly replaced. Runtime stats/enable/remark updates do not
// touch the generation. The SQL expression is atomic, so concurrent control-
// plane updates cannot compute the same next revision from a stale cache copy.
func (k *ChannelKey) BeforeUpdate(tx *gorm.DB) error {
	if tx == nil || tx.Statement == nil || !tx.Statement.Changed("ChannelKey") {
		return nil
	}
	tx.Statement.SetColumn("credential_revision", gorm.Expr(
		"CASE WHEN credential_revision < 1 THEN 2 ELSE credential_revision + 1 END",
	))
	return nil
}
