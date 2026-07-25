#include "ft_btree.h"

t_btree	*build_fixed_tree(void)
{
	t_btree	*d;

	d = btree_create_node("d");
	d->left = btree_create_node("b");
	d->right = btree_create_node("f");
	d->left->left = btree_create_node("a");
	d->left->right = btree_create_node("c");
	d->right->right = btree_create_node("g");
	return (d);
}

void	free_tree(t_btree *root)
{
	if (!root)
		return ;
	free_tree(root->left);
	free_tree(root->right);
	free(root);
}
