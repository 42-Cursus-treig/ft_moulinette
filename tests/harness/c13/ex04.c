#include "ft_btree.h"
#include <unistd.h>

void	btree_insert_data(t_btree **root, void *item,
			int (*cmpf)(void *, void *));

static void	free_tree(t_btree *root)
{
	if (!root)
		return ;
	free_tree(root->left);
	free_tree(root->right);
	free(root);
}

static int	ft_strcmp(void *a, void *b)
{
	char	*s1;
	char	*s2;
	int		i;

	s1 = (char *)a;
	s2 = (char *)b;
	i = 0;
	while (s1[i] && s1[i] == s2[i])
		i++;
	return ((unsigned char)s1[i] - (unsigned char)s2[i]);
}

static void	print_infix(t_btree *root)
{
	char	*s;
	int		i;

	if (!root)
		return ;
	print_infix(root->left);
	s = (char *)root->item;
	i = 0;
	while (s[i])
		i++;
	write(1, s, i);
	write(1, " ", 1);
	print_infix(root->right);
}

int	main(int argc, char **argv)
{
	t_btree	*root;
	int		i;

	root = NULL;
	i = 1;
	while (i < argc)
	{
		btree_insert_data(&root, argv[i], &ft_strcmp);
		i++;
	}
	print_infix(root);
	write(1, "\n", 1);
	free_tree(root);
	return (0);
}
